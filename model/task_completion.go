package model

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TaskBillingAdjustment adjusts an already counted submission. It never adds
// another request, and its quota update shares the statistics transaction.
type TaskBillingAdjustment struct {
	TaskID        int64 `json:"task_id,omitempty"`
	MidjourneyID  int   `json:"midjourney_id,omitempty"`
	PreviousQuota int   `json:"previous_quota"`
	FinalQuota    int   `json:"final_quota"`
}

func TaskBillingRequestID(taskID int64) string {
	return "task-terminal:" + strconv.FormatInt(taskID, 10)
}

// PersistTaskCompletion commits the terminal transition and its accounting
// intent together. A losing poller cannot replace the winner's result or charge.
func PersistTaskCompletion(ctx context.Context, task *Task, fromStatus TaskStatus, actualQuota int, consumption BillingConsumption) (bool, error) {
	if DB == nil || task == nil || task.ID <= 0 || actualQuota < 0 || actualQuota > common.MaxQuota ||
		(task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure) ||
		(task.Status == TaskStatusFailure && actualQuota != 0) {
		return false, errors.New("invalid task completion")
	}
	won := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
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
		if err := createTaskCompletionBilling(tx, TaskBillingRequestID(current.ID), current.PrivateData, consumption); err != nil {
			return err
		}
		privateData := current.PrivateData
		privateData.ResultURL = task.PrivateData.ResultURL
		result := tx.Model(&current).Where("status = ?", fromStatus).Updates(map[string]any{
			"status": task.Status, "progress": task.Progress, "start_time": task.StartTime, "finish_time": task.FinishTime,
			"submit_time": task.SubmitTime, "fail_reason": task.FailReason, "data": task.Data,
			"private_data": privateData, "updated_at": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingRecoveryStateConflict
		}
		won = true
		return nil
	})
	return won && err == nil, err
}

// Both task tables use the same funding, token and consumption recovery. Only
// their terminal compare-and-swap and quota target differ.
func createTaskCompletionBilling(tx *gorm.DB, requestID string, privateData TaskPrivateData, consumption BillingConsumption) error {
	adjustment := consumption.TaskAdjustment
	if adjustment == nil || adjustment.PreviousQuota < 0 || adjustment.PreviousQuota > common.MaxQuota ||
		adjustment.FinalQuota < 0 || adjustment.FinalQuota > common.MaxQuota ||
		(adjustment.TaskID > 0) == (adjustment.MidjourneyID > 0) ||
		consumption.Log.UserId <= 0 || consumption.Log.TokenId != privateData.TokenId {
		return ErrBillingRecoveryStateConflict
	}
	source := privateData.BillingSource
	if source == "" {
		source = "wallet"
	}
	if source != "wallet" {
		return ErrUnsupportedBillingSource
	}
	tokenRequired := privateData.TokenId > 0
	if submissionID := privateData.SubmissionRequestID; submissionID != "" {
		var submission BillingRecovery
		if err := tx.Where("request_id = ?", submissionID).First(&submission).Error; err != nil {
			return err
		}
		if submission.UserID != consumption.Log.UserId || submission.TokenID != privateData.TokenId ||
			submission.Source != source ||
			submission.Status != BillingRecoveryStatusSettled || submission.ConsumptionState != BillingRecoveryStateCompleted {
			return ErrBillingRecoveryStateConflict
		}
		tokenRequired = submission.TokenRequired
	}
	delta := adjustment.FinalQuota - adjustment.PreviousQuota
	consumption.Log.RequestId = requestID
	consumption.Log.Quota = delta
	consumption.Log.Type = LogTypeConsume
	if delta < 0 {
		consumption.Log.Quota = -delta
		consumption.Log.Type = LogTypeRefund
	}
	if delta == 0 {
		consumption.LogEnabled, consumption.ExportEnabled, consumption.CountUsage = false, false, false
	}
	data, err := common.Marshal(consumption)
	if err != nil {
		return err
	}
	recovery := BillingRecovery{
		RequestID: requestID, UserID: consumption.Log.UserId, TokenID: privateData.TokenId, TokenRequired: tokenRequired,
		Source: source, SettlementDelta: int64(delta),
		PreConsumeState: BillingRecoveryPreConsumeCommitted, Status: BillingRecoveryStatusSettlementPending,
		ConsumptionSnapshot: string(data), ConsumptionState: BillingRecoveryStatePending,
		ConsumptionStatistics: BillingRecoveryStatePending, ConsumptionLog: BillingRecoveryStatePending,
	}
	return tx.Create(&recovery).Error
}
