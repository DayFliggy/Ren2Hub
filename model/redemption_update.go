package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// A used redemption code records one wallet credit (its principal, bonus,
// redeemer and time), so administrators may only edit codes that are unused.
// Only Redeem moves a code to the used status.

var (
	ErrRedemptionUsed          = common.NewMessage("Used redemption codes cannot be modified")
	ErrRedemptionStatusInvalid = common.NewMessage("Redemption code status must be enabled or disabled")
)

// UpdateUnusedRedemption changes the editable fields of an unused code.
func UpdateUnusedRedemption(id int, name string, quota int, expiredTime int64) (*Redemption, error) {
	return updateUnusedRedemption(id, quota, map[string]any{"name": name, "quota": quota, "expired_time": expiredTime})
}

// SetRedemptionStatus enables or disables an unused code.
func SetRedemptionStatus(id int, status int) (*Redemption, error) {
	if status != common.RedemptionCodeStatusEnabled && status != common.RedemptionCodeStatusDisabled {
		return nil, ErrRedemptionStatusInvalid
	}
	return updateUnusedRedemption(id, 0, map[string]any{"status": status})
}

// updateUnusedRedemption applies updates unless the code is used. A quota of 0
// keeps the stored quota, which must still be a valid wallet amount.
func updateUnusedRedemption(id int, quota int, updates map[string]any) (*Redemption, error) {
	var redemption Redemption
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).First(&redemption, "id = ?", id).Error; err != nil {
			return err
		}
		if redemption.Status == common.RedemptionCodeStatusUsed {
			return ErrRedemptionUsed
		}
		if quota == 0 {
			quota = redemption.Quota
		}
		if quota <= 0 {
			return common.NewMessage("redemption quota must be positive")
		}
		if err := common.ValidateWalletQuota(quota); err != nil {
			return err
		}
		// The status predicate also guards SQLite, which has no row lock: a
		// concurrent Redeem that won first leaves this update without effect.
		if err := tx.Model(&Redemption{}).Where("id = ? AND status <> ?", id, common.RedemptionCodeStatusUsed).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&redemption, "id = ?", id).Error
	})
	if err != nil {
		return nil, err
	}
	if redemption.Status == common.RedemptionCodeStatusUsed {
		return nil, ErrRedemptionUsed
	}
	return &redemption, nil
}
