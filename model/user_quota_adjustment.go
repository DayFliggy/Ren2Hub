package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	ErrInvalidUserQuotaAdjustment = errors.New("invalid user quota adjustment")
	ErrUserQuotaPermission        = errors.New("cannot adjust quota for this user role")
)

// UserQuotaAdjustment is the immutable database snapshot of a committed manual
// adjustment. Pending relay deductions in the quota cache are not part of it.
type UserQuotaAdjustment struct {
	UserID   int
	Username string
	Before   int
	After    int
	Replayed bool
}

type AdminQuotaOperation struct {
	ID           int    `json:"id"`
	OperatorID   int    `json:"operator_id"`
	OperationID  string `json:"operation_id" gorm:"type:varchar(96)"`
	OperationKey string `json:"-" gorm:"type:varchar(128);uniqueIndex;not null"`
	UserID       int    `json:"user_id" gorm:"index"`
	Value        int    `json:"value" gorm:"type:bigint"`
	Before       int    `json:"before" gorm:"type:bigint"`
	After        int    `json:"after" gorm:"type:bigint"`
	CreatedAt    int64  `json:"created_at"`
}

// AdjustUserQuota is retained for non-recharge internal corrections.
func AdjustUserQuota(userID, operatorRole int, mode string, value int) (*UserQuotaAdjustment, error) {
	return adjustUserQuota(userID, 0, operatorRole, mode, value, "")
}
func AdjustUserQuotaWithOperation(userID, operatorID, operatorRole int, mode string, value int, operationID string) (*UserQuotaAdjustment, error) {
	if mode == "add" && (!affiliateOperationPattern.MatchString(operationID) || operatorID <= 0) {
		return nil, ErrAffiliateOperation
	}
	return adjustUserQuota(userID, operatorID, operatorRole, mode, value, operationID)
}
func adjustUserQuota(userID, operatorID, operatorRole int, mode string, value int, operationID string) (*UserQuotaAdjustment, error) {
	if userID <= 0 || (mode != "add" && mode != "subtract" && mode != "override") {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if mode != "override" && value <= 0 {
		return nil, ErrInvalidUserQuotaAdjustment
	}
	if value > common.MaxWalletQuota || value < -common.MaxWalletQuota {
		return nil, ErrWalletQuotaLimitExceeded
	}

	var adjustment UserQuotaAdjustment
	err := DB.Transaction(func(tx *gorm.DB) error {
		locked, err := lockAffiliateParticipantsTx(tx, userID)
		if err != nil {
			return err
		}
		user := *locked
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		var operation AdminQuotaOperation
		operationKey := fmt.Sprintf("%d:%s", operatorID, operationID)
		if mode == "add" && operatorID > 0 {
			err := lockForUpdate(tx).Where("operation_key = ?", operationKey).First(&operation).Error
			if err == nil {
				if operation.UserID != userID || operation.Value != value {
					return ErrAffiliateOperationConflict
				}
				adjustment = UserQuotaAdjustment{UserID: userID, Username: user.Username, Before: operation.Before, After: operation.After, Replayed: true}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if user.Quota > common.MaxWalletQuota || user.Quota < -common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		quota := decimal.NewFromInt(int64(value))
		switch mode {
		case "add":
			quota = decimal.NewFromInt(int64(user.Quota)).Add(quota)
		case "subtract":
			quota = decimal.NewFromInt(int64(user.Quota)).Sub(quota)
		}
		after, err := common.WalletQuotaFromDecimalStrict(quota)
		if err != nil {
			return ErrWalletQuotaLimitExceeded
		}
		// An unchanged override is a successful operation, including on MySQL
		// configurations that count only changed rows in RowsAffected.
		if after != user.Quota {
			result := tx.Model(&User{}).Where("id = ?", userID).Update("quota", after)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		if mode == "add" && operatorID > 0 {
			operation = AdminQuotaOperation{OperatorID: operatorID, OperationID: operationID, OperationKey: operationKey, UserID: userID, Value: value, Before: user.Quota, After: after, CreatedAt: common.GetTimestamp()}
			if err := tx.Create(&operation).Error; err != nil {
				return err
			}
			if err := applyAffiliateRewardTx(tx, &user, "admin_quota", fmt.Sprint(operation.ID), value); err != nil {
				return err
			}
		}
		adjustment = UserQuotaAdjustment{UserID: user.Id, Username: user.Username, Before: user.Quota, After: after}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Apply only the committed difference, preserving outstanding reservations.
	// Both balances are bounded above, so their difference fits in int64.
	delta := int64(adjustment.After) - int64(adjustment.Before)
	if delta != 0 && !adjustment.Replayed {
		if err := cacheIncrUserQuota(userID, delta); err != nil {
			common.SysError(common.LogText("failed to sync manual quota adjustment for user %d: %s", userID, err))
		}
	}
	return &adjustment, nil
}
