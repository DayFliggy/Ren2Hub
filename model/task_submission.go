package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// TaskSubmissionReady keeps polling refunds and completion adjustments behind
// the original submission's funding, token and consumption recovery.
func TaskSubmissionReady(ctx context.Context, task *Task) (bool, error) {
	if task == nil {
		return false, errors.New("missing task")
	}
	if task.PrivateData.SubmissionRequestID == "" {
		return true, nil
	}
	var recovery BillingRecovery
	if err := DB.WithContext(ctx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error; err != nil {
		return false, err
	}
	if recovery.UserID != task.UserId || recovery.TokenID != task.PrivateData.TokenId {
		return false, ErrBillingRecoveryStateConflict
	}
	return recovery.Status == BillingRecoveryStatusSettled && recovery.ConsumptionState == BillingRecoveryStateCompleted, nil
}

// PersistAcceptedTask saves the provider job and its accounting intent together.
// Recovery may settle the accepted submission without ever replaying it upstream.
func PersistAcceptedTask(task *Task, requestID string, consumption BillingConsumption) error {
	if DB == nil || task == nil || task.TaskID == "" || task.PrivateData.UpstreamTaskID == "" ||
		task.UserId != consumption.Log.UserId || task.ChannelId != consumption.Log.ChannelId || task.Quota != consumption.Log.Quota {
		return errors.New("invalid accepted task")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := prepareBillingRecoveryConsumption(tx, requestID, consumption, true); err != nil {
			return err
		}
		var existing Task
		err := tx.Where("task_id = ? AND user_id = ?", task.TaskID, task.UserId).First(&existing).Error
		if err == nil {
			if existing.ChannelId != task.ChannelId || existing.Quota != task.Quota ||
				existing.PrivateData.UpstreamTaskID != task.PrivateData.UpstreamTaskID {
				return errors.New("accepted task identity conflict")
			}
			task.ID = existing.ID
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(task).Error
	})
}
