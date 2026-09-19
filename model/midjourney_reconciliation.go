package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// HoldMidjourneyBilling saves a confirmed provider terminal result together
// with an unresolved charge. A pricing error cannot replay or refund the job.
func HoldMidjourneyBilling(ctx context.Context, task *Midjourney, fromStatus, reason string) error {
	if task == nil || task.Id <= 0 || task.PrivateData.SubmissionRequestID == "" || reason == "" ||
		(task.Status != "SUCCESS" && task.Status != "FAILURE") {
		return ErrBillingRecoveryStateConflict
	}
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error; err != nil {
			return err
		}
		var current Midjourney
		if err := lockForUpdate(tx).First(&current, task.Id).Error; err != nil {
			return err
		}
		if current.Status != fromStatus || current.Status == "SUCCESS" || current.Status == "FAILURE" {
			return nil
		}
		if current.PrivateData.SubmissionRequestID != recovery.RequestID || current.UserId != recovery.UserID ||
			current.PrivateData.TokenId != recovery.TokenID || current.UserId != task.UserId || current.MjId != task.MjId ||
			current.ChannelId != task.ChannelId || (recovery.Status != BillingRecoveryStatusActive && recovery.Status != BillingRecoveryStatusReconciliation) {
			return ErrBillingRecoveryStateConflict
		}
		consumption, err := taskReconciliationConsumption(recovery, &TaskBillingAdjustment{MidjourneyID: current.Id, PreviousQuota: current.Quota})
		if err != nil {
			return err
		}
		updates := map[string]any{"status": BillingRecoveryStatusReconciliation, "last_error": reason, "consumption_snapshot": consumption, "updated_at": common.GetTimestamp(), "version": recovery.Version + 1}
		if snapshot := task.PrivateData.BillingContext; snapshot != nil && snapshot.Usage != nil {
			data, err := common.Marshal(snapshot.Usage)
			if err != nil {
				return err
			}
			updates["usage_snapshot"] = string(data)
		}
		if err := tx.Model(&recovery).Updates(updates).Error; err != nil {
			return err
		}
		// Preserve the original ownership and quote even if evidence supplied by
		// the current poller is incomplete. The review retains the known usage.
		return tx.Model(&current).Updates(map[string]any{
			"code": task.Code, "status": task.Status, "progress": task.Progress, "start_time": task.StartTime,
			"finish_time": task.FinishTime, "submit_time": task.SubmitTime, "fail_reason": task.FailReason,
			"prompt_en": task.PromptEn, "description": task.Description, "state": task.State,
			"image_url": task.ImageUrl, "video_url": task.VideoUrl, "video_urls": task.VideoUrls,
			"buttons": task.Buttons, "properties": task.Properties,
		}).Error
	})
}
