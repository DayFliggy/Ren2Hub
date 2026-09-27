package model

import "slices"

const BillingRecoveryStateUsagePending = "usage_pending"
const BillingRecoveryStateUsageCollecting = "usage_collect"

func billingRecoveryUnconfirmedStates() []string {
	return []string{BillingRecoveryStateUsagePending, BillingRecoveryStateUsageCollecting,
		BillingRecoveryStatePricingPending, BillingRecoveryStatePricingCollecting, BillingRecoveryStateUnprepared}
}

func BillingRecoveryUsageUnconfirmed(state string) bool {
	return slices.Contains(billingRecoveryUnconfirmedStates(), state)
}

func BillingRecoveryUsageCollecting(state string) bool {
	return state == BillingRecoveryStateUsageCollecting || state == BillingRecoveryStatePricingCollecting
}
