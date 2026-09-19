package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// HoldTaskBilling records missing evidence without inventing a provider failure.
// A confirmed terminal response may still resolve this hold through normal polling.
func HoldTaskBilling(ctx context.Context, task *Task, from TaskStatus, reason string) error {
	if task == nil || task.ID == 0 || task.PrivateData.SubmissionRequestID == "" {
		return ErrBillingRecoveryStateConflict
	}
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error; err != nil {
			return err
		}
		var current Task
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
			return err
		}
		if current.Status != from || current.Status == TaskStatusSuccess || current.Status == TaskStatusFailure {
			return nil
		}
		if current.PrivateData.SubmissionRequestID != task.PrivateData.SubmissionRequestID {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.Status != BillingRecoveryStatusActive && recovery.Status != BillingRecoveryStatusReconciliation {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.LastError != reason || recovery.Status != BillingRecoveryStatusReconciliation {
			consumption, err := taskReconciliationConsumption(recovery, &TaskBillingAdjustment{TaskID: current.ID, PreviousQuota: current.Quota})
			if err != nil {
				return err
			}
			updates := map[string]any{"status": BillingRecoveryStatusReconciliation, "last_error": reason, "consumption_snapshot": consumption, "version": recovery.Version + 1, "updated_at": common.GetTimestamp()}
			if context := task.PrivateData.BillingContext; context != nil && context.Usage != nil {
				data, err := common.Marshal(context.Usage)
				if err != nil {
					return err
				}
				updates["usage_snapshot"] = string(data)
			}
			if err := tx.Model(&recovery).Updates(updates).Error; err != nil {
				return err
			}
		}
		if task.Status == TaskStatusSuccess || task.Status == TaskStatusFailure {
			return tx.Model(&current).Updates(map[string]any{"status": task.Status, "progress": task.Progress, "finish_time": task.FinishTime, "data": task.Data, "private_data": task.PrivateData, "fail_reason": task.FailReason, "updated_at": common.GetTimestamp()}).Error
		}
		return nil
	})
}

// The provider task is terminal even when its cost needs an operator decision.
// Retain its identity so delivery can update its displayed cost atomically.
func taskReconciliationConsumption(recovery BillingRecovery, adjustment *TaskBillingAdjustment) (string, error) {
	var consumption BillingConsumption
	if recovery.ConsumptionSnapshot != "" {
		if err := common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &consumption); err != nil {
			return "", err
		}
	}
	consumption.TaskAdjustment = adjustment
	data, err := common.Marshal(consumption)
	return string(data), err
}
