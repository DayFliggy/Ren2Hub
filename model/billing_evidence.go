package model

import (
	"errors"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"gorm.io/gorm"
)

func billingEvidenceCipher() (*billing.EvidenceCipher, error) {
	secret, explicit := os.LookupEnv("CRYPTO_SECRET")
	if !explicit || secret == "" {
		return nil, errors.New("CRYPTO_SECRET must be explicitly configured for persistent billing evidence")
	}
	return billing.NewEvidenceCipher([]byte(secret))
}

func billingEvidenceBinding(quote *billing.Quote, requestID, attemptID, phase string) (billing.EvidenceBinding, error) {
	if phase != "estimate" && phase != "settlement" {
		return billing.EvidenceBinding{}, billing.ErrEvaluationEvidence
	}
	return billing.EvidenceBinding{RequestID: requestID, AttemptID: attemptID, ConfigHash: billing.PricingConfigHash(quote), Phase: phase}, nil
}

func EvaluateBilling(quote *billing.Quote, usage billing.Usage, input billingexpr.RequestInput, requestID, attemptID, phase string) (billing.Usage, *billing.ChargeResult, error) {
	cipher, err := billingEvidenceCipher()
	if err != nil {
		return usage, nil, err
	}
	binding, err := billingEvidenceBinding(quote, requestID, attemptID, phase)
	if err != nil {
		return usage, nil, err
	}
	return cipher.Evaluate(quote, usage, input, binding)
}

func ReplayBilling(quote *billing.Quote, usage billing.Usage, requestID, attemptID, phase string) (*billing.ChargeResult, error) {
	cipher, err := billingEvidenceCipher()
	if err != nil {
		return nil, err
	}
	binding, err := billingEvidenceBinding(quote, requestID, attemptID, phase)
	if err != nil {
		return nil, err
	}
	return cipher.Replay(quote, usage, binding)
}

func validateBillingAttemptEstimate(attempt BillingAttempt, quote *billing.Quote) error {
	if attempt.EstimateUsage == "" || attempt.EstimateCharge == "" {
		return billing.ErrEvaluationEvidence
	}
	var usage billing.Usage
	var charge billing.ChargeResult
	if common.UnmarshalJsonStr(string(attempt.EstimateUsage), &usage) != nil || common.UnmarshalJsonStr(attempt.EstimateCharge, &charge) != nil || !usage.Estimated {
		return billing.ErrEvaluationEvidence
	}
	replayed, err := ReplayBilling(quote, usage, attempt.RequestID, attempt.ID, "estimate")
	if err != nil {
		return err
	}
	want, err := billing.Hash(replayed)
	if err != nil {
		return err
	}
	got, err := billing.Hash(charge)
	if err != nil {
		return err
	}
	if want != got {
		return ErrBillingEventConflict
	}
	return nil
}

// PersistBillingEstimate is the last evidence barrier before dispatch. An
// attempt's quote and estimate are immutable even if another retry follows.
func PersistBillingEstimate(attemptID string, usage billing.Usage, charge *billing.ChargeResult) error {
	if DB == nil || attemptID == "" || charge == nil || !usage.Estimated {
		return billing.ErrEvaluationEvidence
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var attempt BillingAttempt
		if err := tx.First(&attempt, "id = ?", attemptID).Error; err != nil {
			return err
		}
		var recovery BillingRecovery
		if err := lockForUpdate(tx).First(&recovery, "request_id = ?", attempt.RequestID).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).First(&attempt, "id = ?", attemptID).Error; err != nil {
			return err
		}
		if recovery.Status != BillingRecoveryStatusActive || recovery.PreConsumeState != BillingRecoveryPreConsumeCommitted {
			return ErrBillingRecoveryStateConflict
		}
		currentID, err := billingRecoveryAttemptID(recovery)
		if err != nil {
			return err
		}
		if currentID != "" && currentID != attemptID {
			return ErrBillingEventConflict
		}
		var quote billing.Quote
		if err := common.UnmarshalJsonStr(attempt.Quote, &quote); err != nil {
			return billing.ErrEvaluationEvidence
		}
		if err := validatePublishedBillingQuoteTx(tx, &quote); err != nil {
			return err
		}
		replayed, err := ReplayBilling(&quote, usage, attempt.RequestID, attempt.ID, "estimate")
		if err != nil {
			return err
		}
		expected, err := billing.Hash(replayed)
		if err != nil {
			return err
		}
		actual, err := billing.Hash(charge)
		if err != nil {
			return err
		}
		if expected != actual {
			return ErrBillingEventConflict
		}
		if charge.FinalQuota < 0 || charge.FinalQuota > recovery.FundingAmount {
			return ErrBillingRecoveryFundingQuota
		}
		usageJSON, err := common.Marshal(usage)
		if err != nil {
			return err
		}
		chargeJSON, err := common.Marshal(charge)
		if err != nil {
			return err
		}
		if attempt.EstimateUsage != "" || attempt.EstimateCharge != "" {
			if string(attempt.EstimateUsage) != string(usageJSON) || attempt.EstimateCharge != string(chargeJSON) {
				return ErrBillingEventConflict
			}
			return nil
		}
		return tx.Model(&attempt).Updates(map[string]any{"estimate_usage": LongText(usageJSON), "estimate_charge": string(chargeJSON)}).Error
	})
}
