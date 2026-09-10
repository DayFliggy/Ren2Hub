package model

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrTaskSettlementConflict = errors.New("task terminal settlement conflicts with accepted result")

type TaskSettlementInput struct {
	FinalQuota int
	Refund     bool
	Log        RecordTaskBillingLogParams
}

// Completion and the accepted billing intent are committed together, so a
// process can stop immediately after the status transition without losing money.
func UpdateTaskWithSettlement(ctx context.Context, task *Task, from TaskStatus, input TaskSettlementInput) (bool, error) {
	if task == nil || task.ID <= 0 {
		return false, errors.New("task must be persisted")
	}
	if task.Status != TaskStatusSuccess && task.Status != TaskStatusFailure {
		return false, errors.New("settlement requires a terminal task status")
	}
	if input.Refund != (task.Status == TaskStatusFailure) {
		return false, errors.New("task status and settlement operation conflict")
	}
	won := false
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
			return err
		}
		if current.Status != from {
			return nil
		}
		if current.UserId != task.UserId || current.ChannelId != task.ChannelId || current.PrivateData.TokenId != task.PrivateData.TokenId {
			return errors.New("task completion identity changed")
		}
		receipt, err := prepareTaskSettlementTx(tx, &current, input.FinalQuota, input.Refund, input.Log)
		if err != nil {
			return err
		}
		task.BillingState = "pending"
		if receipt.Applied {
			task.BillingState = current.BillingState
		}
		if err := tx.Model(task).Select("*").Omit("quota").Updates(task).Error; err != nil {
			return err
		}
		won = true
		return nil
	})
	return won, err
}

// The intent survives a failed funds transaction. Amounts and terminal outcome
// are immutable; cache and log recovery never repeat the accounting transaction.
type TaskSettlement struct {
	TaskID        int64  `gorm:"primaryKey;autoIncrement:false"`
	UserID        int    `gorm:"index"`
	TokenID       int    `gorm:"index"`
	Operation     string `gorm:"size:16;not null"`
	BeforeQuota   int    `gorm:"not null"`
	FinalQuota    int    `gorm:"not null"`
	Delta         int    `gorm:"not null"`
	TokenMissing  bool
	UserVersion   int64 `gorm:"index"`
	TokenVersion  int64 `gorm:"index"`
	CreatedAt     int64
	Applied       bool `gorm:"index"`
	CacheSynced   bool
	LogDelivered  bool
	LogRequired   bool
	ExportEnabled bool
	LogPayload    string `gorm:"type:text"`
}

func prepareTaskSettlementTx(tx *gorm.DB, task *Task, finalQuota int, refund bool, params RecordTaskBillingLogParams) (TaskSettlement, error) {
	var receipt TaskSettlement
	if task == nil || task.ID <= 0 || finalQuota < 0 || finalQuota > common.MaxQuota || task.Quota < 0 || task.Quota > common.MaxQuota {
		return receipt, errors.New("invalid task settlement amount or identity")
	}
	operation := "settle"
	if refund {
		operation, finalQuota = "refund", 0
	}
	err := tx.First(&receipt, "task_id = ?", task.ID).Error
	if err == nil {
		if receipt.Operation != operation || receipt.FinalQuota != finalQuota || receipt.UserID != task.UserId || receipt.TokenID != task.PrivateData.TokenId {
			return receipt, ErrTaskSettlementConflict
		}
		return receipt, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return receipt, err
	}
	if task.PrivateData.BillingSource != "wallet" && task.PrivateData.BillingSource != "subscription" {
		return receipt, errors.New("task funding source requires reconciliation")
	}
	payload, err := common.Marshal(params)
	if err != nil {
		return receipt, err
	}
	receipt = TaskSettlement{
		TaskID: task.ID, UserID: task.UserId, TokenID: task.PrivateData.TokenId,
		Operation: operation, BeforeQuota: task.Quota, FinalQuota: finalQuota,
		Delta: finalQuota - task.Quota, CreatedAt: time.Now().Unix(), LogPayload: string(payload),
		LogRequired:   finalQuota != task.Quota && (finalQuota < task.Quota || common.LogConsumeEnabled),
		ExportEnabled: common.DataExportEnabled,
	}
	return receipt, tx.Create(&receipt).Error
}

func ApplyTaskSettlement(ctx context.Context, task *Task, finalQuota int, refund bool, params RecordTaskBillingLogParams) (TaskSettlement, error) {
	var receipt TaskSettlement
	if task == nil || task.ID <= 0 {
		return receipt, errors.New("task settlement requires a persisted task")
	}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current Task
		if err := lockForUpdate(tx).First(&current, task.ID).Error; err != nil {
			return err
		}
		if current.UserId != task.UserId || current.ChannelId != task.ChannelId || current.PrivateData.TokenId != task.PrivateData.TokenId {
			return errors.New("task settlement identity changed")
		}
		var err error
		receipt, err = prepareTaskSettlementTx(tx, &current, finalQuota, refund, params)
		return err
	})
	if err != nil {
		return receipt, err
	}
	receipt, err = ResumeTaskSettlement(ctx, receipt.TaskID)
	if receipt.Applied {
		task.Quota = receipt.FinalQuota
	}
	return receipt, err
}

func ResumeTaskSettlement(ctx context.Context, taskID int64) (TaskSettlement, error) {
	var receipt TaskSettlement
	if err := DB.WithContext(ctx).First(&receipt, "task_id = ?", taskID).Error; err != nil {
		return receipt, err
	}
	if receipt.Applied {
		return receipt, nil
	}
	unlock := lockQuotaMutationResources(receipt.UserID, receipt.TokenID)
	defer unlock()
	if err := FlushBatchUpdates(ctx); err != nil {
		return receipt, err
	}
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		if err := lockForUpdate(tx).First(&task, taskID).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&receipt, "task_id = ?", taskID).Error; err != nil {
			return err
		}
		if receipt.Applied {
			return nil
		}
		if task.Quota != receipt.BeforeQuota || task.UserId != receipt.UserID || task.PrivateData.TokenId != receipt.TokenID {
			return ErrTaskSettlementConflict
		}
		if receipt.Delta != 0 {
			if err := applyTaskAccountingTx(tx, &task, &receipt); err != nil {
				return err
			}
		}
		state := "settled"
		if receipt.Operation == "refund" {
			state = "refunded"
		}
		if err := tx.Model(&Task{}).Where("id = ?", taskID).Updates(map[string]any{"quota": receipt.FinalQuota, "billing_state": state}).Error; err != nil {
			return err
		}
		receipt.Applied = true
		return tx.Save(&receipt).Error
	})
	if err != nil {
		// An uncertain commit is resolved by the next durable read, not this copy.
		return TaskSettlement{}, err
	}
	return receipt, nil
}

func taskQuotaAfter(value int, delta int64) (int, error) {
	result := int64(value) + delta
	if result < math.MinInt32 || result > common.MaxQuota {
		return 0, errors.New("task accounting amount out of range; reconciliation required")
	}
	return int(result), nil
}

func applyTaskAccountingTx(tx *gorm.DB, task *Task, receipt *TaskSettlement) error {
	var user User
	if err := lockForUpdate(tx.Unscoped()).First(&user, task.UserId).Error; err != nil {
		return err
	}
	used, err := taskQuotaAfter(user.UsedQuota, int64(receipt.Delta))
	if err != nil {
		return err
	}
	updates := map[string]any{"used_quota": used}
	switch task.PrivateData.BillingSource {
	case "wallet":
		quota, err := taskQuotaAfter(user.Quota, -int64(receipt.Delta))
		if err != nil {
			return err
		}
		if user.SettlementVersion >= maxSettlementVersion {
			return errors.New("task wallet settlement version exhausted")
		}
		receipt.UserVersion = user.SettlementVersion + 1
		if err := fenceTaskSettlementCache(tx.Statement.Context, "user", user.Id, receipt.UserVersion); err != nil {
			return err
		}
		updates["quota"], updates["settlement_version"] = quota, receipt.UserVersion
	case "subscription":
		var sub UserSubscription
		if task.PrivateData.SubscriptionId <= 0 {
			return errors.New("task subscription identity missing")
		}
		if err := lockForUpdate(tx).First(&sub, task.PrivateData.SubscriptionId).Error; err != nil {
			return err
		}
		if sub.UserId != task.UserId || sub.AmountUsed < 0 || receipt.Delta > 0 && sub.AmountUsed > math.MaxInt64-int64(receipt.Delta) {
			return errors.New("invalid task subscription accounting")
		}
		newUsed := sub.AmountUsed + int64(receipt.Delta)
		if newUsed < 0 || sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
			return errors.New("task subscription amount requires reconciliation")
		}
		if err := tx.Model(&sub).Update("amount_used", newUsed).Error; err != nil {
			return err
		}
	default:
		return errors.New("task funding source requires reconciliation")
	}
	if err := tx.Unscoped().Model(&user).Updates(updates).Error; err != nil {
		return err
	}
	if task.PrivateData.TokenId > 0 {
		var token Token
		err := lockForUpdate(tx.Unscoped()).First(&token, task.PrivateData.TokenId).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			receipt.TokenMissing = true
		case err != nil:
			return err
		default:
			if token.UserId != task.UserId || token.SettlementVersion >= maxSettlementVersion {
				return errors.New("invalid task token accounting identity or version")
			}
			remain, err := taskQuotaAfter(token.RemainQuota, -int64(receipt.Delta))
			if err != nil {
				return err
			}
			used, err := taskQuotaAfter(token.UsedQuota, int64(receipt.Delta))
			if err != nil {
				return err
			}
			receipt.TokenVersion = token.SettlementVersion + 1
			if err := fenceTaskSettlementCache(tx.Statement.Context, "token", token.Id, receipt.TokenVersion); err != nil {
				return err
			}
			if err := tx.Unscoped().Model(&token).Updates(map[string]any{
				"remain_quota": remain, "used_quota": used, "settlement_version": receipt.TokenVersion,
			}).Error; err != nil {
				return err
			}
		}
	}
	if task.ChannelId > 0 {
		var channel Channel
		if err := lockForUpdate(tx).First(&channel, task.ChannelId).Error; err != nil {
			return err
		}
		if receipt.Delta > 0 && channel.UsedQuota > math.MaxInt64-int64(receipt.Delta) || receipt.Delta < 0 && channel.UsedQuota < math.MinInt64-int64(receipt.Delta) {
			return errors.New("task channel accounting overflow")
		}
		if err := tx.Model(&channel).Update("used_quota", channel.UsedQuota+int64(receipt.Delta)).Error; err != nil {
			return err
		}
	}
	if receipt.ExportEnabled {
		var params RecordTaskBillingLogParams
		if err := common.UnmarshalJsonStr(receipt.LogPayload, &params); err != nil {
			return err
		}
		// An adjustment changes the total but does not create another request.
		return tx.Create(&QuotaData{UserID: user.Id, Username: user.Username, ModelName: params.ModelName,
			CreatedAt: receipt.CreatedAt - receipt.CreatedAt%3600, TokenID: receipt.TokenID,
			ChannelID: task.ChannelId, NodeName: params.NodeName, Quota: receipt.Delta}).Error
	}
	return nil
}
