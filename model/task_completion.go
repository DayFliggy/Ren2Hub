package model

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
)

// TaskBillingAdjustment adjusts an already counted submission. It never adds
// another request, and its quota update shares the statistics transaction.
type TaskBillingAdjustment struct {
	TaskID        int64        `json:"task_id,omitempty"`
	MidjourneyID  int          `json:"midjourney_id,omitempty"`
	PreviousQuota common.Quota `json:"previous_quota"`
	FinalQuota    common.Quota `json:"final_quota"`
}

func TaskBillingRequestID(taskID int64) string {
	return "task-terminal:" + strconv.FormatInt(taskID, 10)
}

// PersistTaskCompletion commits the terminal transition and its accounting
// intent together. A losing poller cannot replace the winner's result or charge.
func PersistTaskCompletion(ctx context.Context, task *Task, fromStatus TaskStatus, actualQuota common.Quota, consumption BillingConsumption) (bool, error) {
	if DB == nil || task == nil || task.ID <= 0 || actualQuota < 0 || actualQuota > common.MaxQuota ||
		(task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure) ||
		(task.Status == TaskStatusFailure && actualQuota != 0) {
		return false, errors.New("invalid task completion")
	}
	won := false
	err := BillingTransaction(ctx, func(tx *gorm.DB) error {
		won = false
		// Lock request state before its task row, matching recovery delivery.
		var guard BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&guard).Error; err != nil {
			return err
		}
		var current Task
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
			return err
		}
		if err := validateTaskCompletionIdentity(&current, task); err != nil {
			return err
		}
		if current.Status != fromStatus || current.Status == TaskStatusSuccess || current.Status == TaskStatusFailure {
			return nil
		}
		if current.UserId != task.UserId || current.ChannelId != task.ChannelId || current.Quota != task.Quota || current.Quota < 0 || current.Quota > common.MaxQuota ||
			current.PrivateData.TokenId != consumption.Log.TokenId || consumption.Log.UserId != current.UserId || consumption.Log.ChannelId != current.ChannelId {
			return ErrBillingRecoveryStateConflict
		}
		consumption.TaskAdjustment = &TaskBillingAdjustment{TaskID: current.ID, PreviousQuota: current.Quota, FinalQuota: actualQuota}
		if err := createTaskCompletionBilling(tx, TaskBillingRequestID(current.ID), current.PrivateData, consumption, task.Status == TaskStatusFailure); err != nil {
			return err
		}
		privateData := current.PrivateData
		privateData.ResultURL = task.PrivateData.ResultURL
		privateData.PluginState = task.PrivateData.PluginState
		privateData.PollFailures = task.PrivateData.PollFailures
		privateData.CompletionUsage = task.PrivateData.CompletionUsage
		privateData.BillingContext = task.PrivateData.BillingContext
		privateData.ResultDiscarded = task.PrivateData.ResultDiscarded
		storedData := task.Data
		if privateData.ResultDiscarded {
			storedData = nil
			privateData.PluginState = nil
			privateData.ResultURL = ""
		}
		result := tx.Model(&current).Where("status = ?", fromStatus).Updates(map[string]any{
			"status": task.Status, "progress": task.Progress, "start_time": task.StartTime, "finish_time": task.FinishTime,
			"submit_time": task.SubmitTime, "fail_reason": task.FailReason, "data": storedData,
			"private_data": privateData, "updated_at": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingRecoveryStateConflict
		}
		if err := recordTaskResultEvent(tx, task); err != nil {
			return err
		}
		won = true
		return nil
	})
	return won && err == nil, err
}

// Both task tables use the same funding, token and consumption recovery. Only
// their terminal compare-and-swap and quota target differ.
func createTaskCompletionBilling(tx *gorm.DB, _ string, privateData TaskPrivateData, consumption BillingConsumption, release bool) error {
	adjustment := consumption.TaskAdjustment
	if adjustment == nil || privateData.SubmissionRequestID == "" || adjustment.FinalQuota < 0 || adjustment.FinalQuota > common.MaxQuota {
		return ErrBillingRecoveryStateConflict
	}
	var recovery BillingRecovery
	if err := lockForUpdate(tx).Where("request_id = ?", privateData.SubmissionRequestID).First(&recovery).Error; err != nil {
		return err
	}
	if privateData.BillingSource != "wallet" || recovery.Source != "wallet" {
		return ErrUnsupportedBillingSource
	}
	if recovery.UserID != consumption.Log.UserId || recovery.TokenID != privateData.TokenId || (recovery.Status != BillingRecoveryStatusActive && recovery.Status != BillingRecoveryStatusReconciliation) || recovery.ConsumptionState != BillingRecoveryStateTaskPending {
		return ErrBillingRecoveryStateConflict
	}
	consumption.Log.RequestId = recovery.RequestID
	consumption.Log.Quota = common.Quota(adjustment.FinalQuota)
	consumption.Log.Type = LogTypeConsume
	if release {
		consumption.CountUsage = false
		consumption.Log.Type = LogTypeRefund
	}
	data, err := common.Marshal(consumption)
	if err != nil {
		return err
	}
	status := BillingRecoveryStatusSettlementPending
	if release {
		status = BillingRecoveryStatusRefundPending
	}
	updates := map[string]any{"status": status, "settlement_delta": common.Quota(adjustment.FinalQuota) - recovery.FundingAmount, "consumption_snapshot": string(data), "consumption_state": BillingRecoveryStatePending, "consumption_statistics": BillingRecoveryStatePending, "consumption_log": BillingRecoveryStatePending, "updated_at": common.GetTimestamp(), "version": recovery.Version + 1}
	if consumption.Usage != nil {
		usage, err := common.Marshal(consumption.Usage)
		if err != nil {
			return err
		}
		updates["usage_snapshot"] = string(usage)
	}
	if consumption.Charge != nil {
		if consumption.Charge.FinalQuota != adjustment.FinalQuota {
			return ErrBillingEventConflict
		}
		charge, err := common.Marshal(consumption.Charge)
		if err != nil {
			return err
		}
		updates["charge_snapshot"] = string(charge)
		var quote billing.Quote
		if err := common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote); err != nil {
			return err
		}
		evidenceHash, err := BillingEvidenceFingerprint(recovery.RequestID, quote.Hash, consumption.Usage, consumption.Charge)
		if err != nil {
			return err
		}
		updates["consumption_fingerprint"] = evidenceHash
	}
	return tx.Model(&recovery).Updates(updates).Error
}
