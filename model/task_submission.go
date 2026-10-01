package model

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const BillingRecoveryStateTaskPending = "task_pending"

func prepareTaskAcceptedBillingTx(tx *gorm.DB, requestID string, consumption BillingConsumption) error {
	var recovery BillingRecovery
	if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return err
	}
	if recovery.Status != BillingRecoveryStatusActive || recovery.PreConsumeState != BillingRecoveryPreConsumeCommitted || recovery.UserID != consumption.Log.UserId || recovery.TokenID != consumption.Log.TokenId {
		return ErrBillingRecoveryStateConflict
	}
	data, err := common.Marshal(consumption)
	if err != nil {
		return err
	}
	if recovery.ConsumptionState == BillingRecoveryStateTaskPending {
		if recovery.ConsumptionSnapshot != string(data) {
			return ErrBillingEventConflict
		}
		return nil
	}
	return tx.Model(&recovery).Updates(map[string]any{"consumption_snapshot": string(data), "consumption_state": BillingRecoveryStateTaskPending, "version": recovery.Version + 1, "updated_at": common.GetTimestamp()}).Error
}

// TaskSubmissionReady keeps polling refunds and completion adjustments behind
// the original submission's funding, token and consumption recovery.
func TaskSubmissionReady(ctx context.Context, task *Task) (bool, error) {
	if task == nil {
		return false, errors.New("missing task")
	}
	if task.PrivateData.SubmissionRequestID == "" {
		return false, ErrBillingRecoveryStateConflict
	}
	var recovery BillingRecovery
	if err := DB.WithContext(ctx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error; err != nil {
		return false, err
	}
	if recovery.UserID != task.UserId || recovery.TokenID != task.PrivateData.TokenId {
		return false, ErrBillingRecoveryStateConflict
	}
	return recovery.Status == BillingRecoveryStatusActive && recovery.ConsumptionState == BillingRecoveryStateTaskPending, nil
}

// PersistAcceptedTask saves the provider job and its accounting intent together.
// Recovery may settle the accepted submission without ever replaying it upstream.
func PersistAcceptedTask(task *Task, requestID string, consumption BillingConsumption) error {
	if DB == nil || task == nil || task.TaskID == "" || task.PrivateData.UpstreamTaskID == "" ||
		task.UserId != consumption.Log.UserId || task.ChannelId != consumption.Log.ChannelId || task.Quota != consumption.Log.Quota {
		return errors.New("invalid accepted task")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := prepareTaskAcceptedBillingTx(tx, requestID, consumption); err != nil {
			return err
		}
		if err := AttachTaskPluginExecution(tx, task); err != nil {
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
		if task.PrivateData.ResultDiscarded {
			return tx.Omit("data").Create(task).Error
		}
		return tx.Create(task).Error
	})
}
