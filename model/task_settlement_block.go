package model

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
)

// TaskSettlementBlockedEvidence is the reason code of a task whose provider
// reported a successful result that cannot be settled because the attempt's
// billing evidence no longer verifies, for example after a Go toolchain change
// or an altered attempt row. Such a task keeps its reservation and its result;
// neither the frozen estimate nor the timeout sweep may end it.
const TaskSettlementBlockedEvidence = "billing_evidence_unverifiable"

// ErrTaskSettlementBlocked reports a durable task whose result is held until
// settlement succeeds.
var ErrTaskSettlementBlocked = errors.New("task settlement is blocked: billing evidence cannot be verified")

// settlementNotBlocked selects rows without a held result. Rows created before
// the column existed hold NULL.
const settlementNotBlocked = "(settlement_block IS NULL OR settlement_block = '')"

// TaskBlockedResult is the provider terminal result of a settlement-blocked
// task. It stays private until settlement commits and copies it onto the task.
type TaskBlockedResult struct {
	Status     TaskStatus      `json:"status"`
	Progress   string          `json:"progress,omitempty"`
	FailReason string          `json:"fail_reason,omitempty"`
	FinishTime int64           `json:"finish_time,omitempty"`
	ResultURL  string          `json:"result_url,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
}

// IsBillingEvidenceUnverifiable reports the integrity failures that block a
// settlement: evidence that cannot be decrypted, decoded or replayed, or that
// replays to a different charge or attempt. Retrying the same evidence in the
// same process cannot succeed.
func IsBillingEvidenceUnverifiable(err error) bool {
	return errors.Is(err, billing.ErrEvaluationEvidence) || errors.Is(err, ErrBillingEventConflict)
}

// PersistTaskSettlementBlock holds a successful provider result on its
// accepted, still non-terminal task. The reservation stays held because the
// submission recovery must remain task_pending. It reports whether the task
// was newly blocked; an already blocked task only moves to the back of the
// retry order.
func PersistTaskSettlementBlock(ctx context.Context, task *Task, fromStatus TaskStatus, held TaskBlockedResult) (bool, error) {
	if DB == nil || task == nil || task.ID <= 0 || fromStatus == TaskStatusSuccess || fromStatus == TaskStatusFailure || held.Status != TaskStatusSuccess {
		return false, errors.New("invalid task settlement block")
	}
	encoded, err := common.Marshal(held)
	if err != nil {
		return false, err
	}
	newly := false
	err = BillingTransaction(ctx, func(tx *gorm.DB) error {
		newly = false
		// Lock request state before its task row, matching completion.
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error; err != nil {
			return err
		}
		var current Task
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
			return err
		}
		if err := validateTaskCompletionIdentity(&current, task); err != nil {
			return err
		}
		if current.Status != fromStatus {
			return nil
		}
		if recovery.Status != BillingRecoveryStatusActive || recovery.ConsumptionState != BillingRecoveryStateTaskPending {
			return ErrBillingRecoveryStateConflict
		}
		privateData := current.PrivateData
		privateData.BlockedResult = encoded
		privateData.CompletionUsage = task.PrivateData.CompletionUsage
		privateData.PluginState = task.PrivateData.PluginState
		privateData.PollFailures = 0
		result := tx.Model(&current).Where("status = ?", fromStatus).Updates(map[string]any{
			"settlement_block": TaskSettlementBlockedEvidence, "private_data": privateData, "updated_at": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingRecoveryStateConflict
		}
		newly = current.SettlementBlock == ""
		return nil
	})
	return newly, err
}

// PersistBlockedImmediateTask saves an immediate result whose settlement is
// blocked as an accepted, non-terminal task holding that result, with the same
// submission accounting intent as any accepted task.
func PersistBlockedImmediateTask(ctx context.Context, task *Task, submission BillingConsumption, held TaskBlockedResult) error {
	if DB == nil || task == nil || task.ID != 0 || task.TaskID == "" || task.PrivateData.UpstreamTaskID == "" ||
		task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure || held.Status != TaskStatusSuccess {
		return errors.New("invalid blocked immediate task")
	}
	encoded, err := common.Marshal(held)
	if err != nil {
		return err
	}
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		accepted := *task
		accepted.SettlementBlock = TaskSettlementBlockedEvidence
		accepted.PrivateData.BlockedResult = encoded
		if err := persistAcceptedTaskTx(tx, &accepted, task.PrivateData.SubmissionRequestID, submission); err != nil {
			return err
		}
		task.ID, task.CreatedAt, task.UpdatedAt = accepted.ID, accepted.CreatedAt, accepted.UpdatedAt
		task.SettlementBlock, task.PrivateData.BlockedResult = accepted.SettlementBlock, encoded
		return nil
	})
}

// GetSettlementBlockedTasks returns blocked tasks least recently retried first,
// so a fixed batch size cannot starve any of them.
func GetSettlementBlockedTasks(ctx context.Context, limit int) ([]*Task, error) {
	var tasks []*Task
	err := DB.WithContext(ctx).Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Where("settlement_block <> ''").Order("updated_at").Order("id").Limit(limit).Find(&tasks).Error
	return tasks, err
}

// PersistMidjourneySettlementBlock is PersistTaskSettlementBlock for the
// Midjourney table; held is the provider update that reported success.
func PersistMidjourneySettlementBlock(ctx context.Context, task *Midjourney, fromStatus string, held json.RawMessage) (bool, error) {
	if DB == nil || task == nil || task.Id <= 0 || task.PrivateData.SubmissionRequestID == "" || fromStatus == "SUCCESS" || fromStatus == "FAILURE" || len(held) == 0 {
		return false, errors.New("invalid Midjourney settlement block")
	}
	newly := false
	err := BillingTransaction(ctx, func(tx *gorm.DB) error {
		newly = false
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error; err != nil {
			return err
		}
		var current Midjourney
		if err := lockForUpdate(tx).First(&current, task.Id).Error; err != nil {
			return err
		}
		if current.Status != fromStatus {
			return nil
		}
		if current.PrivateData.SubmissionRequestID != task.PrivateData.SubmissionRequestID || current.UserId != task.UserId ||
			current.ChannelId != task.ChannelId || current.MjId != task.MjId {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.Status != BillingRecoveryStatusActive || recovery.ConsumptionState != BillingRecoveryStateTaskPending {
			return ErrBillingRecoveryStateConflict
		}
		privateData := current.PrivateData
		privateData.BlockedResult = held
		result := tx.Model(&current).Where("status = ?", fromStatus).Updates(map[string]any{
			"settlement_block": TaskSettlementBlockedEvidence, "private_data": privateData,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrBillingRecoveryStateConflict
		}
		newly = current.SettlementBlock == ""
		return nil
	})
	return newly, err
}

// CountSettlementBlocked counts unfinished tasks and Midjourney jobs whose
// provider result is held behind a blocked settlement.
func CountSettlementBlocked(ctx context.Context) (int64, error) {
	if DB == nil {
		return 0, errors.New("task database is unavailable")
	}
	db := DB.WithContext(ctx)
	var tasks, midjourney int64
	if err := db.Model(&Task{}).Where("status NOT IN ?", []string{TaskStatusFailure, TaskStatusSuccess}).
		Where("settlement_block <> ''").Count(&tasks).Error; err != nil {
		return 0, err
	}
	if err := db.Model(&Midjourney{}).Where("(status IS NULL OR status NOT IN ?)", []string{"SUCCESS", "FAILURE"}).
		Where("settlement_block <> ''").Count(&midjourney).Error; err != nil {
		return 0, err
	}
	return tasks + midjourney, nil
}
