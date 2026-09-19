package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type ActivityClaim struct {
	ID          int          `json:"id" gorm:"primaryKey"`
	UserID      int          `json:"user_id" gorm:"uniqueIndex:idx_activity_claim_user_key"`
	ActivityKey string       `json:"activity_key" gorm:"type:varchar(64);uniqueIndex:idx_activity_claim_user_key"`
	Reward      common.Quota `json:"reward" gorm:"type:bigint;not null"`
	CreatedAt   int64        `json:"created_at" gorm:"autoCreateTime"`
}

func (ActivityClaim) TableName() string { return "activity_claims" }

func HasActivityClaim(userID int, activityKey string) (bool, error) {
	var count int64
	err := DB.Model(&ActivityClaim{}).
		Where("user_id = ? AND activity_key = ?", userID, activityKey).
		Count(&count).Error
	return count > 0, err
}

func ClaimActivityReward(userID int, activityKey string, reward common.Quota) error {
	if userID <= 0 || activityKey == "" || reward <= 0 {
		return errors.New("invalid activity reward claim")
	}
	claim := ActivityClaim{
		UserID:      userID,
		ActivityKey: activityKey,
		Reward:      reward,
		CreatedAt:   time.Now().Unix(),
	}
	err := BillingTransaction(nil, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&ActivityClaim{}).
			Where("user_id = ? AND activity_key = ?", userID, activityKey).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errors.New("activity reward already claimed")
		}
		if err := tx.Create(&claim).Error; err != nil {
			return err
		}
		_, err := ApplyWalletAdjustmentTx(tx, WalletAdjustment{EventID: fmt.Sprintf("activity:%d", claim.ID), UserID: userID, Delta: reward, SourceType: "activity", SourceID: activityKey, Reason: "activity reward claimed"})
		return err
	})
	if err != nil {
		return err
	}
	return nil
}
