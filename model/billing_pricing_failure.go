package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"gorm.io/gorm"
)

const BillingRecoveryStatusReconciliation = "reconciliation_required"
const BillingRecoveryStatePricingPending = "pricing_pending"
const BillingRecoveryStatePricingCollecting = "pricing_collect"

// This is a review record, never a consumption intent. Request bodies, headers
// and raw evaluator errors may contain secrets and are deliberately excluded.
type BillingPricingFailure struct {
	ChannelID int                         `json:"channel_id"`
	Model     string                      `json:"model"`
	Snapshot  billingexpr.BillingSnapshot `json:"pricing"`
	Tokens    billingexpr.TokenParams     `json:"tokens"`
	Reason    string                      `json:"reason"`
}

// A durable guard precedes upstream dispatch. Only explicit failure refunds or
// confirmed consumption can resolve it; a lost process cannot imply free usage.
func PrepareBillingPricing(requestID string, pending BillingPricingFailure) error {
	if DB == nil || requestID == "" || pending.Snapshot.BillingMode != "tiered_expr" {
		return errors.New("invalid billing pricing preparation")
	}
	pending.Reason = "billing_expression_usage_unconfirmed"
	return prepareBillingDispatch(requestID, pending, BillingRecoveryStatePricingPending)
}

func RecordBillingPricingFailure(requestID string, failure BillingPricingFailure) error {
	if DB == nil || requestID == "" || failure.Snapshot.BillingMode != "tiered_expr" {
		return errors.New("invalid billing pricing failure")
	}
	data, err := common.Marshal(failure)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status == BillingRecoveryStatusReconciliation {
			if recovery.ConsumptionSnapshot != string(data) {
				return ErrBillingRecoveryStateConflict
			}
			return nil
		}
		if recovery.Status != BillingRecoveryStatusActive {
			return ErrBillingRecoveryStateConflict
		}
		return tx.Model(&recovery).Updates(map[string]any{
			"status":               BillingRecoveryStatusReconciliation,
			"consumption_snapshot": string(data),
			"consumption_state":    BillingRecoveryStateUnprepared,
			"last_error":           failure.Reason,
			"consumption_error":    failure.Reason,
			"updated_at":           common.GetTimestamp(),
		}).Error
	})
}
