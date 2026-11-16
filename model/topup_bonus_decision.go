package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// A top-up bonus adds quota on top of the principal of a credit: an online
// top-up, a redemption or an administrator's addition. Each source keeps its
// own bonus_quota as the funding fact; the decision record only explains it.
// See docs/billing/TOPUP_BONUS.md.

const (
	TopUpBonusSourceTopUp      = "topup"
	TopUpBonusSourceRedemption = "redemption"
	TopUpBonusSourceAdminQuota = "admin_quota"
)

// TopUpBonusGrant is the bonus decided for one credit. Decision is the frozen
// JSON explanation; it is empty when no bonus component is configured, and
// then nothing is recorded.
type TopUpBonusGrant struct {
	BonusQuota    int
	ConfigVersion int64
	Decision      string
}

// TopUpBonusEvaluator decides the bonus of one credit of principal quota. It
// is prepared before the credit's transaction and must not query the
// database; it returns the error of loading its inputs, if any.
type TopUpBonusEvaluator func(principal int) (TopUpBonusGrant, error)

// grantTopUpBonus evaluates principal; a nil evaluator grants nothing.
func grantTopUpBonus(evaluate TopUpBonusEvaluator, principal int) (TopUpBonusGrant, error) {
	if evaluate == nil {
		return TopUpBonusGrant{}, nil
	}
	grant, err := evaluate(principal)
	if err != nil {
		return TopUpBonusGrant{}, err
	}
	// The cap is at most 0.5, so a bonus never exceeds its principal.
	if grant.BonusQuota < 0 || grant.BonusQuota > principal {
		return TopUpBonusGrant{}, ErrInvalidTopUpQuota
	}
	return grant, nil
}

// TopUpBonusDecisionRecord explains the bonus of one credit to administrators.
// No credit or settlement ever reads it.
type TopUpBonusDecisionRecord struct {
	SourceType    string `json:"source_type" gorm:"type:varchar(24);primaryKey"`
	SourceID      string `json:"source_id" gorm:"type:varchar(64);primaryKey"`
	UserID        int    `json:"user_id" gorm:"index;not null"`
	ConfigVersion int64  `json:"config_version" gorm:"not null"`
	Decision      string `json:"decision" gorm:"type:text;not null"`
	CreatedAt     int64  `json:"created_at" gorm:"not null"`
}

func (TopUpBonusDecisionRecord) TableName() string {
	return "topup_bonus_decisions"
}

// insertTopUpBonusDecisionTx stores the explanation in its credit's transaction.
func insertTopUpBonusDecisionTx(tx *gorm.DB, sourceType string, sourceID int, userID int, grant *TopUpBonusGrant) error {
	if grant == nil || grant.Decision == "" {
		return nil
	}
	return tx.Create(&TopUpBonusDecisionRecord{
		SourceType: sourceType, SourceID: strconv.Itoa(sourceID), UserID: userID,
		ConfigVersion: grant.ConfigVersion, Decision: grant.Decision, CreatedAt: common.GetTimestamp(),
	}).Error
}

// insertTopUpBonusTx stores a new order's decision in the order's transaction.
func insertTopUpBonusTx(tx *gorm.DB, order *TopUp) error {
	return insertTopUpBonusDecisionTx(tx, TopUpBonusSourceTopUp, order.Id, order.UserId, order.Bonus)
}

// GetTopUpBonusDecision returns the explanation of one credit, if any.
func GetTopUpBonusDecision(sourceType string, sourceID int) (*TopUpBonusDecisionRecord, error) {
	var rows []TopUpBonusDecisionRecord
	err := DB.Where("source_type = ? AND source_id = ?", sourceType, strconv.Itoa(sourceID)).Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// SuccessfulTopUpsBetween lists successful orders of the given providers
// created in [start, end), for the bonus estimate.
func SuccessfulTopUpsBetween(start, end int64, providers []string) ([]TopUp, error) {
	var rows []TopUp
	err := DB.Select("id", "user_id", "create_time", "expected_quota", "bonus_quota").
		Where("status = ? AND create_time >= ? AND create_time < ? AND payment_provider IN ?", common.TopUpStatusSuccess, start, end, providers).
		Find(&rows).Error
	return rows, err
}

// RedeemedCodesBetween lists codes redeemed in [start, end), including codes
// deleted after use.
func RedeemedCodesBetween(start, end int64) ([]Redemption, error) {
	var rows []Redemption
	err := DB.Unscoped().Select("id", "used_user_id", "quota", "redeemed_time").
		Where("status = ? AND redeemed_time >= ? AND redeemed_time < ?", common.RedemptionCodeStatusUsed, start, end).
		Find(&rows).Error
	return rows, err
}

// AdminQuotaAddsBetween lists administrators' additions made in [start, end).
// An empty mode is an addition recorded before modes were stored.
func AdminQuotaAddsBetween(start, end int64) ([]AdminQuotaOperation, error) {
	var rows []AdminQuotaOperation
	err := DB.Select("id", "user_id", "value", "created_at").
		Where("operator_id > 0 AND mode IN ? AND created_at >= ? AND created_at < ?", []string{"add", ""}, start, end).
		Find(&rows).Error
	return rows, err
}
