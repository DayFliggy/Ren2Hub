package model

import (
	"context"
	"errors"
	"fmt"

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
	return taskSubmissionMatches(task, &recovery)
}

// TaskSubmissionsReady applies TaskSubmissionReady to a polling batch with
// one recovery query per chunk instead of one per task. Tasks whose recovery
// is missing or inconsistent are left out and reported in the returned error.
func TaskSubmissionsReady(ctx context.Context, tasks []*Task) ([]*Task, error) {
	const chunk = 500
	recoveries := make(map[string]*BillingRecovery, len(tasks))
	for start := 0; start < len(tasks); start += chunk {
		ids := make([]string, 0, chunk)
		for _, task := range tasks[start:min(start+chunk, len(tasks))] {
			if task.PrivateData.SubmissionRequestID != "" {
				ids = append(ids, task.PrivateData.SubmissionRequestID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		var rows []*BillingRecovery
		if err := DB.WithContext(ctx).Where("request_id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, row := range rows {
			recoveries[row.RequestID] = row
		}
	}
	ready := make([]*Task, 0, len(tasks))
	var result error
	for _, task := range tasks {
		requestID := task.PrivateData.SubmissionRequestID
		recovery := recoveries[requestID]
		if requestID == "" || recovery == nil {
			result = errors.Join(result, fmt.Errorf("task %s: submission accounting unavailable", task.TaskID))
			continue
		}
		ok, err := taskSubmissionMatches(task, recovery)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("task %s: %w", task.TaskID, err))
			continue
		}
		if ok {
			ready = append(ready, task)
		}
	}
	return ready, result
}

func taskSubmissionMatches(task *Task, recovery *BillingRecovery) (bool, error) {
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
		return persistAcceptedTaskTx(tx, task, requestID, consumption)
	})
}

func persistAcceptedTaskTx(tx *gorm.DB, task *Task, requestID string, consumption BillingConsumption) error {
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
	stored := *task
	stored.Data, stored.PrivateData = taskResultForStorage(task.Data, task.PrivateData)
	if err := tx.Create(&stored).Error; err != nil {
		return err
	}
	task.ID, task.CreatedAt, task.UpdatedAt = stored.ID, stored.CreatedAt, stored.UpdatedAt
	return nil
}

// PersistImmediateTask leaves no polling-visible non-terminal task between
// accepting an immediate result and preparing its terminal accounting intent.
// A rollback retains the already durable pre-send estimate for recovery.
func PersistImmediateTask(ctx context.Context, task *Task, fromStatus TaskStatus, actualQuota common.Quota, submission, completion BillingConsumption) (bool, error) {
	if DB == nil || task == nil || task.ID != 0 || task.TaskID == "" || task.PrivateData.UpstreamTaskID == "" ||
		task.UserId != submission.Log.UserId || task.ChannelId != submission.Log.ChannelId || task.Quota != submission.Log.Quota ||
		fromStatus == TaskStatusSuccess || fromStatus == TaskStatusFailure {
		return false, errors.New("invalid immediate task")
	}
	committed := false
	err := BillingTransaction(ctx, func(tx *gorm.DB) error {
		committed = false
		accepted := *task
		accepted.ID = 0
		accepted.Status = fromStatus
		if err := persistAcceptedTaskTx(tx, &accepted, task.PrivateData.SubmissionRequestID, submission); err != nil {
			return err
		}
		task.ID = accepted.ID
		task.CreatedAt, task.UpdatedAt = accepted.CreatedAt, accepted.UpdatedAt
		var err error
		committed, err = persistTaskCompletionTx(tx, task, fromStatus, actualQuota, completion)
		if err == nil && !committed {
			return ErrBillingRecoveryStateConflict
		}
		return err
	})
	if err != nil {
		task.ID = 0
	}
	return committed && err == nil, err
}
