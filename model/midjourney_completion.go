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
// accounting intent together. The final calculated charge is authoritative;
// confirmed failure releases the reservation.
func PersistMidjourneyCompletion(ctx context.Context, task *Midjourney, fromStatus string, consumption BillingConsumption) (bool, error) {
	if DB == nil || task == nil || task.Id <= 0 || (task.Status != "SUCCESS" && task.Status != "FAILURE") {
		return false, errors.New("invalid Midjourney completion")
	}
	if task.PrivateData.SubmissionRequestID == "" {
		return false, ErrBillingRecoveryStateConflict
	}
	won := false
	err := BillingTransaction(ctx, func(tx *gorm.DB) error {
		won = false
		// Lock request state before its task row, matching recovery delivery.
		var guard BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&guard).Error; err != nil {
			return err
		}
		var current Midjourney
		if err := lockForUpdate(tx).First(&current, task.Id).Error; err != nil {
			return err
		}
		if current.Status != fromStatus || current.Status == "SUCCESS" || current.Status == "FAILURE" {
			return nil
		}
		if current.PrivateData.SubmissionRequestID != task.PrivateData.SubmissionRequestID {
			return ErrBillingRecoveryStateConflict
		}
		if current.UserId != task.UserId || current.ChannelId != task.ChannelId || current.MjId != task.MjId ||
			current.Quota != task.Quota || current.Quota < 0 || current.Quota > common.MaxQuota ||
			consumption.Log.UserId != current.UserId || consumption.Log.ChannelId != current.GetBillingChannelId() {
			return ErrBillingRecoveryStateConflict
		}
		privateData := current.PrivateData
		actualQuota := common.Quota(0)
		if task.Status == "SUCCESS" {
			if consumption.Charge == nil || consumption.Usage == nil {
				return ErrBillingRecoveryStateConflict
			}
			actualQuota = consumption.Charge.FinalQuota
		}
		if task.Status == "FAILURE" {
			actualQuota = 0
			consumption.Charge, consumption.Usage = nil, nil
		}
		if privateData.BillingContext != nil && consumption.Charge != nil {
			snapshot := *privateData.BillingContext
			snapshot.Usage, snapshot.Charge = consumption.Usage, consumption.Charge
			privateData.BillingContext = &snapshot
		}
		consumption.TaskAdjustment = &TaskBillingAdjustment{MidjourneyID: current.Id, PreviousQuota: current.Quota, FinalQuota: actualQuota}
		if err := createTaskCompletionBilling(tx, MidjourneyBillingRequestID(current.Id), privateData, consumption, task.Status == "FAILURE"); err != nil {
			return err
		}
		result := tx.Model(&current).Where("status = ?", fromStatus).Updates(map[string]any{
			"code": task.Code, "status": task.Status, "progress": task.Progress, "start_time": task.StartTime,
			"finish_time": task.FinishTime, "submit_time": task.SubmitTime, "fail_reason": task.FailReason,
			"prompt_en": task.PromptEn, "description": task.Description, "state": task.State,
			"image_url": task.ImageUrl, "video_url": task.VideoUrl, "video_urls": task.VideoUrls,
			"buttons": task.Buttons, "properties": task.Properties, "private_data": privateData,
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
