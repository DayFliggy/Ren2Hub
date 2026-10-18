package model

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var topUpCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func validTopUpCurrency(value string) bool { return topUpCurrencyPattern.MatchString(value) }

func legacyTopUpQuota(order *TopUp) (int, error) {
	amount := decimal.NewFromInt(order.Amount)
	if order.PaymentProvider == PaymentProviderStripe {
		amount = decimal.NewFromFloat(order.Money)
	}
	if order.PaymentProvider != PaymentProviderCreem {
		amount = amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	quota, err := common.WalletQuotaFromDecimalStrict(amount)
	if err != nil || quota <= 0 {
		return 0, ErrInvalidTopUpQuota
	}
	return quota, nil
}

// FreezeTopUpQuota records the exact paid credit before the order is sent.
// Discounts change the checkout price; no implicit bonus quota is invented.
func FreezeTopUpQuota(order *TopUp) error {
	if order.ExpectedQuota <= 0 {
		quota, err := legacyTopUpQuota(order)
		if err != nil {
			return err
		}
		order.ExpectedQuota = quota
	}
	if _, err := topUpQuotaMaxCurrent(order.ExpectedQuota); err != nil {
		return err
	}
	order.AffiliateBaseQuota = order.ExpectedQuota
	order.Currency = strings.ToUpper(strings.TrimSpace(order.Currency))
	if order.Currency != "" && !validTopUpCurrency(order.Currency) {
		return fmt.Errorf("invalid order currency")
	}
	if order.PayAmount == "" && order.Currency != "" {
		order.PayAmount = decimal.NewFromFloat(order.Money).String()
	}
	if order.PayAmount != "" {
		value, err := decimal.NewFromString(order.PayAmount)
		if err != nil || !value.IsPositive() {
			return ErrInvalidTopUpQuota
		}
		order.PayAmount = value.String()
		if len(order.PayAmount) > 64 {
			return fmt.Errorf("order payment amount is too large")
		}
	}
	return nil
}

// completeTopUpTx is the sole successful TopUp transition. The caller holds the
// order lock and has already verified its provider receipt. Referral credit and
// wallet credit either commit together, or both roll back.
func completeTopUpTx(tx *gorm.DB, order *TopUp, fallbackQuota int, updates map[string]any) (int, error) {
	if order.Status == common.TopUpStatusSuccess {
		return 0, nil
	}
	if order.Status != common.TopUpStatusPending {
		return 0, ErrTopUpStatusInvalid
	}
	quota := order.ExpectedQuota
	if quota <= 0 {
		quota = fallbackQuota
		// Pending native orders created before these snapshot columns still
		// earn under the rules at their first successful credit, never replay.
		order.ExpectedQuota = quota
		order.AffiliateBaseQuota = quota
	}
	if _, err := topUpQuotaMaxCurrent(quota); err != nil {
		return 0, err
	}
	user, err := lockAffiliateParticipantsTx(tx, order.UserId)
	if err != nil {
		return 0, err
	}
	if err = creditTopUpQuota(tx, order.UserId, quota, updates); err != nil {
		return 0, err
	}
	base := order.AffiliateBaseQuota
	if base < 0 || base > quota {
		return 0, ErrInvalidTopUpQuota
	}
	if err = applyAffiliateRewardTx(tx, user, "topup", fmt.Sprint(order.Id), base); err != nil {
		return 0, err
	}
	order.CompleteTime = common.GetTimestamp()
	order.Status = common.TopUpStatusSuccess
	order.CreditedQuota = quota
	if err = tx.Save(order).Error; err != nil {
		return 0, err
	}
	return quota, nil
}
