package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

// failTasksWithoutChannel ends tasks whose channel row has been deleted.
// Upstream marks them FAILURE in bulk without a refund; this follows the
// per-task CAS and refund rules of sweepTimedOutTasks instead, so only the
// caller that wins the CAS refunds and legacy tasks stay unrefunded.
//
// A cache miss or a failed read is not proof of deletion: unless the database
// reports the row as not found, the tasks stay unchanged and the error is
// returned so the next poll retries.
func failTasksWithoutChannel(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	if _, err := model.GetChannelById(channelId, false); !errors.Is(err, gorm.ErrRecordNotFound) {
		if err == nil {
			return fmt.Errorf("channel #%d still exists; keep its tasks for the next poll", channelId)
		}
		return fmt.Errorf("confirm channel #%d deletion: %w", channelId, err)
	}

	reason := "Failed to get the channel information. Please contact the administrator"
	legacyReason := "Failed to get the channel information (a task left by the old system is not refunded; contact the administrator)"
	now := time.Now().Unix()
	var errs []error

	for _, upstreamID := range taskIds {
		task := taskM[upstreamID]
		if task == nil {
			continue
		}
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < model.TaskRefundLegacyCutoff

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
			task.Quota = 0
		} else {
			task.FailReason = reason
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, common.LogText("failTasksWithoutChannel CAS update error for task %s: %v", task.TaskID, err))
			errs = append(errs, err)
			continue
		}
		if !won {
			logger.LogInfo(ctx, common.LogText("failTasksWithoutChannel: task %s already transitioned, skip", task.TaskID))
			continue
		}
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}
	return errors.Join(errs...)
}
