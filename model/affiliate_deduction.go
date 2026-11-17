package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var (
	ErrAffiliateDeductionInvalid = common.NewMessage("referral deduction amount must be a positive quota")
	ErrAffiliateDeductionExceeds = common.NewMessage("amount exceeds the deductible referral quota")
)

// DeductAffiliateQuota 由管理员扣减 userID 的可用返利，同一 operationID 只扣一次。
//
// 只扣未归因的可用返利（充值返利和历史余额）：仍归因到管理员加额 accrual 的部分
// 由受邀人额度扣减时的冲回处理，扣进这部分会让之后的冲回再记一次欠款。冻结中的
// 返利不能扣，到期的先解冻。钱包、累计返利、冻结、欠款和 accrual 记录都不变，
// 不受活动开关和支付合规开关限制。
func DeductAffiliateQuota(userID, operatorRole, quota int, operationID string) (*AffiliateRecord, bool, error) {
	if !affiliateOperationPattern.MatchString(operationID) {
		return nil, false, ErrAffiliateOperation
	}
	if userID <= 0 || quota <= 0 || quota > common.MaxWalletQuota {
		return nil, false, ErrAffiliateDeductionInvalid
	}
	key := fmt.Sprintf("admin_deduction:%d:%s", userID, operationID)
	var result AffiliateRecord
	replayed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		user, err := lockAffiliateParticipantsTx(tx, userID)
		if err != nil {
			return err
		}
		if operatorRole != common.RoleRootUser && operatorRole <= user.Role {
			return ErrUserQuotaPermission
		}
		err = lockForUpdate(tx).Where("source_key = ?", key).First(&result).Error
		if err == nil {
			if result.UserID != userID || result.Kind != "deduction" || result.SourceType != "admin_deduction" || result.SourceID != operationID || result.RewardQuota != quota {
				return ErrAffiliateOperationConflict
			}
			replayed = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := common.GetTimestamp()
		if err = thawAffiliateTx(tx, user, now); err != nil {
			return err
		}
		_, tracked, err := trackedAffiliateAvailableTx(tx, user)
		if err != nil {
			return err
		}
		if quota > user.AffQuota-tracked {
			return ErrAffiliateDeductionExceeds
		}
		user.AffQuota -= quota
		if err = tx.Model(&User{}).Where("id = ?", userID).Update("aff_quota", user.AffQuota).Error; err != nil {
			return err
		}
		result = AffiliateRecord{UserID: userID, Kind: "deduction", SourceType: "admin_deduction", SourceID: operationID, SourceKey: key, RewardQuota: quota, AvailableAfter: user.AffQuota, FrozenAfter: user.AffFrozenQuota, CreatedAt: now}
		return tx.Create(&result).Error
	})
	if err != nil {
		return nil, false, err
	}
	return &result, replayed, nil
}
