package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrAffiliateNoReward = common.NewMessage("this order has no referral reward to reverse")

// TopUpAffiliateReward is the referral reward one top-up order paid, as shown
// in the administrator order detail.
type TopUpAffiliateReward struct {
	InviterID   int  `json:"inviter_id"`
	RewardQuota int  `json:"reward_quota"`
	Reversed    bool `json:"reversed"`
}

func topUpAffiliateKey(topUpID int) string {
	return fmt.Sprintf("topup:%d", topUpID)
}

func topUpAffiliateReversalKey(topUpID int) string {
	return fmt.Sprintf("topup_reversal:%d", topUpID)
}

// GetTopUpAffiliateReward 返回订单发出的返利；没有发出返利时返回 nil。
func GetTopUpAffiliateReward(topUpID int) (*TopUpAffiliateReward, error) {
	var accrual AffiliateRecord
	err := DB.Where("source_key = ? AND kind = ? AND reward_quota > 0", topUpAffiliateKey(topUpID), "accrue").First(&accrual).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reversals int64
	if err = DB.Model(&AffiliateRecord{}).Where("source_key = ?", topUpAffiliateReversalKey(topUpID)).Count(&reversals).Error; err != nil {
		return nil, err
	}
	return &TopUpAffiliateReward{InviterID: accrual.UserID, RewardQuota: accrual.RewardQuota, Reversed: reversals > 0}, nil
}

// ReverseTopUpAffiliateReward 回收一笔充值订单发给邀请人的返利，每个订单只回收一次。
//
// 冻结中的返利从冻结余额扣回。已解冻的充值返利没有划转归因，只能从未归因的可用返利
// 扣回，扣不到的部分（已划转）记为欠款，由以后的返利抵扣；该返利当初
// 抵掉的旧欠款加回去。仍归因到管理员加额的返利、累计返利、单人上限、受邀人的钱包和
// 订单状态都不变；不受活动开关和支付合规开关限制。记录字段的含义与管理员冲回相同。
func ReverseTopUpAffiliateReward(tradeNo string, operatorRole int) (*AffiliateRecord, bool, error) {
	var order TopUp
	if err := DB.Where("trade_no = ?", tradeNo).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, common.NewMessage("Top-up order does not exist")
		}
		return nil, false, err
	}
	key := topUpAffiliateKey(order.Id)
	var result AffiliateRecord
	replayed := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		var accrual AffiliateRecord
		if err := tx.Where("source_key = ? AND kind = ?", key, "accrue").First(&accrual).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAffiliateNoReward
			}
			return err
		}
		if accrual.RewardQuota <= 0 {
			return ErrAffiliateNoReward
		}
		// User rows before referral records, under the shared lock order. A
		// soft-deleted inviter is still reversible.
		users, err := lockAffiliateUsersTx(tx, accrual.InviteeID, accrual.UserID)
		if err != nil {
			return err
		}
		var inviter *User
		for i := range users {
			if users[i].Id == accrual.UserID {
				inviter = &users[i]
			}
		}
		if inviter == nil {
			return ErrAffiliateNoReward
		}
		if operatorRole != common.RoleRootUser && operatorRole <= inviter.Role {
			return ErrUserQuotaPermission
		}
		reversalKey := topUpAffiliateReversalKey(order.Id)
		err = lockForUpdate(tx).Where("source_key = ?", reversalKey).First(&result).Error
		if err == nil {
			if result.UserID != inviter.Id || result.Kind != "reversal" || result.SourceType != "topup_reversal" || result.RelatedRecordID != accrual.ID {
				return errors.New("affiliate source conflicts with the recorded decision")
			}
			replayed = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		now := common.GetTimestamp()
		if err = thawAffiliateTx(tx, inviter, now); err != nil {
			return err
		}
		// Thawing only updates the stored row, so read the accrual again.
		if err = lockForUpdate(tx).First(&accrual, accrual.ID).Error; err != nil {
			return err
		}
		credited := creditedAffiliateQuota(accrual)
		removed := credited
		if accrual.FrozenUntil > 0 && accrual.ThawedAt == 0 {
			if removed > inviter.AffFrozenQuota {
				return errors.New("affiliate frozen balance is inconsistent with ledger")
			}
			inviter.AffFrozenQuota -= removed
		} else {
			_, tracked, err := trackedAffiliateAvailableTx(tx, inviter)
			if err != nil {
				return err
			}
			removed = min(credited, inviter.AffQuota-tracked)
			inviter.AffQuota -= removed
		}
		debtAdded := accrual.DebtOffsetQuota + credited - removed
		if debtAdded > common.MaxWalletQuota-inviter.AffOffsetDebtQuota {
			return ErrWalletQuotaLimitExceeded
		}
		inviter.AffOffsetDebtQuota += debtAdded
		result = AffiliateRecord{UserID: inviter.Id, InviteeID: accrual.InviteeID, Kind: "reversal", SourceType: "topup_reversal", SourceID: fmt.Sprint(order.Id), SourceKey: reversalKey, RelatedRecordID: accrual.ID, BaseQuota: accrual.BaseQuota, RateBps: accrual.RateBps, RewardQuota: accrual.RewardQuota, CreditedQuota: removed, DebtOffsetQuota: accrual.DebtOffsetQuota, DebtAddedQuota: debtAdded, CreatedAt: now, AvailableAfter: inviter.AffQuota, FrozenAfter: inviter.AffFrozenQuota}
		if err = tx.Create(&result).Error; err != nil {
			return err
		}
		return tx.Unscoped().Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_quota": inviter.AffQuota, "aff_frozen_quota": inviter.AffFrozenQuota, "aff_offset_debt_quota": inviter.AffOffsetDebtQuota}).Error
	})
	if err != nil {
		return nil, false, err
	}
	return &result, replayed, nil
}
