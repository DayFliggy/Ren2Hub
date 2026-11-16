package model

import (
	"gorm.io/gorm"
)

// creditRedemptionTx credits a redemption that has just moved to used: its
// principal plus the bonus decided for it, in the redemption's transaction.
func creditRedemptionTx(tx *gorm.DB, redemption *Redemption, userID int, bonus TopUpBonusEvaluator) error {
	grant, err := grantTopUpBonus(bonus, redemption.Quota)
	if err != nil {
		return err
	}
	if err := creditTopUpQuota(tx, userID, redemption.Quota+grant.BonusQuota, nil); err != nil {
		return err
	}
	redemption.BonusQuota = grant.BonusQuota
	if grant.BonusQuota > 0 {
		if err := tx.Model(&Redemption{}).Where("id = ?", redemption.Id).Update("bonus_quota", grant.BonusQuota).Error; err != nil {
			return err
		}
	}
	return insertTopUpBonusDecisionTx(tx, TopUpBonusSourceRedemption, redemption.Id, userID, &grant)
}
