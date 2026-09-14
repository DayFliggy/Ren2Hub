package model

import (
	"context"
	"errors"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func MidjourneyBillingRequestID(taskID int) string {
	return "midjourney-terminal:" + strconv.Itoa(taskID)
}

// PersistMidjourneyCompletion commits the provider result and its durable
// accounting intent together. Success retains the submission's per-call charge;
// failure refunds it through the same recovery pipeline as other async tasks.
func PersistMidjourneyCompletion(ctx context.Context, task *Midjourney, fromStatus string, consumption BillingConsumption) (bool, error) {
	if DB == nil || task == nil || task.Id <= 0 || (task.Status != "SUCCESS" && task.Status != "FAILURE") {
		return false, errors.New("invalid Midjourney completion")
	}
	won := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Midjourney
		if err := lockForUpdate(tx).First(&current, task.Id).Error; err != nil {
			return err
		}
		if current.Status != fromStatus || current.Status == "SUCCESS" || current.Status == "FAILURE" {
			return nil
		}
		if current.UserId != task.UserId || current.ChannelId != task.ChannelId || current.MjId != task.MjId ||
			current.Quota != task.Quota || current.Quota < 0 || current.Quota > common.MaxQuota ||
			consumption.Log.UserId != current.UserId || consumption.Log.ChannelId != current.GetBillingChannelId() {
			return ErrBillingRecoveryStateConflict
		}
		privateData := current.PrivateData
		if privateData.SubmissionRequestID == "" {
			// Legacy TokenId is an applied-charge marker, not just task ownership.
			// A historical wallet-only charge must never refund an uncharged token.
			privateData.TokenId = current.TokenId
		}
		actualQuota := current.Quota
		if task.Status == "FAILURE" {
			actualQuota = 0
		}
		consumption.TaskAdjustment = &TaskBillingAdjustment{MidjourneyID: current.Id, PreviousQuota: current.Quota, FinalQuota: actualQuota}
		if err := createTaskCompletionBilling(tx, MidjourneyBillingRequestID(current.Id), privateData, consumption); err != nil {
			return err
		}
		result := tx.Model(&current).Where("status = ?", fromStatus).Updates(map[string]any{
			"code": task.Code, "status": task.Status, "progress": task.Progress, "start_time": task.StartTime,
			"finish_time": task.FinishTime, "submit_time": task.SubmitTime, "fail_reason": task.FailReason,
			"prompt_en": task.PromptEn, "description": task.Description, "state": task.State,
			"image_url": task.ImageUrl, "video_url": task.VideoUrl, "video_urls": task.VideoUrls,
			"buttons": task.Buttons, "properties": task.Properties,
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
