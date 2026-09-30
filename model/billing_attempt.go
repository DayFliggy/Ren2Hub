package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
)

// An attempt has its own identity because retries may use different prices.
// The complete quote is immutable even after another attempt is selected.
type BillingAttempt struct {
	EstimateUsage  LongText `json:"-"`
	EstimateCharge string   `json:"-" gorm:"type:text"`
	ID             string   `json:"id" gorm:"type:varchar(128);primaryKey"`
	RequestID      string   `json:"request_id" gorm:"type:varchar(128);not null;index"`
	ChannelID      int      `json:"channel_id"`
	Quote          string   `json:"-" gorm:"type:text;not null"`
	QuoteHash      string   `json:"quote_hash" gorm:"type:varchar(64);not null"`
	Fingerprint    string   `json:"request_fingerprint" gorm:"type:varchar(64);not null"`
	CreatedAt      int64    `json:"created_at" gorm:"type:bigint;not null"`
}

func PrepareBillingAttempt(ctx context.Context, requestID, attemptID string, channelID int, quote *billing.Quote) error {
	return PrepareBillingAttemptWithPlugin(ctx, requestID, attemptID, channelID, quote, nil)
}

func PrepareBillingAttemptWithPlugin(ctx context.Context, requestID, attemptID string, channelID int, quote *billing.Quote, plugin *TaskPluginRef) error {
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		if err := prepareBillingAttemptTx(tx, requestID, attemptID, channelID, quote); err != nil {
			return err
		}
		if plugin != nil {
			if err := ValidateTaskPluginAdmissionWithTx(tx, *plugin); err != nil {
				return err
			}
			return PinTaskPluginAttemptWithTx(tx, requestID, attemptID, *plugin)
		}
		return nil
	})
}

func prepareBillingAttemptTx(tx *gorm.DB, requestID, attemptID string, channelID int, quote *billing.Quote) error {
	if quote == nil || requestID == "" || attemptID == "" {
		return errors.New("complete billing quote is required")
	}
	fingerprint, err := BillingAttemptFingerprint(requestID, channelID, quote)
	if err != nil {
		return err
	}
	data, err := common.Marshal(quote)
	if err != nil {
		return err
	}
	var recovery BillingRecovery
	if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return err
	}
	var existing BillingAttempt
	err = tx.Where("id = ?", attemptID).First(&existing).Error
	if err == nil {
		if existing.Quote != string(data) || existing.RequestID != requestID || existing.ChannelID != channelID {
			return ErrBillingEventConflict
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if recovery.Status != BillingRecoveryStatusActive || recovery.PreConsumeState != BillingRecoveryPreConsumeCommitted || BillingRecoveryUsageCollecting(recovery.ConsumptionState) {
		return ErrBillingRecoveryStateConflict
	}
	if err := tx.Create(&BillingAttempt{ID: attemptID, RequestID: requestID, ChannelID: channelID, Quote: string(data), QuoteHash: quote.Hash, Fingerprint: fingerprint, CreatedAt: common.GetTimestamp()}).Error; err != nil {
		return err
	}
	return tx.Model(&recovery).Updates(map[string]any{"quote_snapshot": string(data), "consumption_state": BillingRecoveryStateUsagePending, "version": recovery.Version + 1, "updated_at": common.GetTimestamp()}).Error
}

// BillingAttemptFingerprint binds an attempt to its logical request and frozen
// charge contract without storing request bodies or credentials. Identical
// bodies with different request IDs are separate, legitimate requests.
func BillingAttemptFingerprint(requestID string, channelID int, quote *billing.Quote) (string, error) {
	if quote == nil || requestID == "" || quote.Hash == "" {
		return "", errors.New("complete billing quote is required")
	}
	copyQuote := *quote
	copyQuote.Hash = ""
	hash, err := billing.Hash(&copyQuote)
	if err != nil || hash != quote.Hash {
		return "", errors.New("billing quote integrity mismatch")
	}
	return billing.Hash(struct {
		RequestID string `json:"request_id"`
		ChannelID int    `json:"channel_id"`
		QuoteHash string `json:"quote_hash"`
	}{requestID, channelID, quote.Hash})
}

func RecordBillingCalculationFailure(requestID string, usage *billing.Usage) error {
	data, err := common.Marshal(usage)
	if err != nil {
		return err
	}
	result := DB.Model(&BillingRecovery{}).Where("request_id = ? AND status IN ?", requestID, []string{BillingRecoveryStatusActive, BillingRecoveryStatusReconciliation}).Updates(map[string]any{"usage_snapshot": string(data), "status": BillingRecoveryStatusReconciliation, "consumption_state": BillingRecoveryStateUnprepared, "last_error": "billing_calculation_invalid", "consumption_error": "billing_calculation_invalid", "version": gorm.Expr("version + 1"), "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrBillingRecoveryStateConflict
	}
	return nil
}
