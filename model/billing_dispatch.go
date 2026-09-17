package model

import (
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
	"gorm.io/gorm"
)

const BillingRecoveryStateUsagePending = "usage_pending"
const BillingRecoveryStateUsageCollecting = "usage_collect"

// Prices aid reconciliation; missing final usage is never reconstructed from
// the precharge. No request body, headers or provider credentials are stored.
type BillingDispatchSnapshot struct {
	ChannelID    int                `json:"channel_id"`
	Model        string             `json:"model"`
	Price        types.PriceData    `json:"price"`
	OtherRatios  map[string]float64 `json:"other_ratios,omitempty"`
	QuotaPerUnit float64            `json:"quota_per_unit"`
}

func billingRecoveryUnconfirmedStates() []string {
	return []string{BillingRecoveryStateUsagePending, BillingRecoveryStateUsageCollecting,
		BillingRecoveryStatePricingPending, BillingRecoveryStatePricingCollecting, BillingRecoveryStateUnprepared}
}

func BillingRecoveryUsageUnconfirmed(state string) bool {
	return slices.Contains(billingRecoveryUnconfirmedStates(), state)
}

func BillingRecoveryUsageCollecting(state string) bool {
	return state == BillingRecoveryStateCollecting || state == BillingRecoveryStateUsageCollecting || state == BillingRecoveryStatePricingCollecting
}

func PrepareBillingDispatch(requestID string, pending BillingDispatchSnapshot) error {
	return prepareBillingDispatch(requestID, pending, BillingRecoveryStateUsagePending)
}

func prepareBillingDispatch(requestID string, pending any, state string) error {
	if DB == nil || requestID == "" {
		return errors.New("invalid billing dispatch preparation")
	}
	data, err := common.Marshal(pending)
	if err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status != BillingRecoveryStatusActive || recovery.PreConsumeState != BillingRecoveryPreConsumeCommitted ||
			BillingRecoveryUsageCollecting(recovery.ConsumptionState) {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.ConsumptionState != "" && !BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
			return ErrBillingRecoveryStateConflict
		}
		return tx.Model(&recovery).Updates(map[string]any{
			"consumption_snapshot": string(data),
			"consumption_state":    state,
			"updated_at":           common.GetTimestamp(),
		}).Error
	})
}
