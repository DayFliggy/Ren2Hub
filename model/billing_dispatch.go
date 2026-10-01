package model

import "slices"

const BillingRecoveryStateUsagePending = "usage_pending"
const BillingRecoveryStateUsageCollecting = "usage_collect"

// These states belong to a request that has not reached its final charge yet.
// Recovery finishes them from the best persisted evidence once they are stale.
func billingRecoveryUnconfirmedStates() []string {
	return []string{BillingRecoveryStateUsagePending, BillingRecoveryStateUsageCollecting, BillingRecoveryStateUnprepared}
}

func BillingRecoveryUsageUnconfirmed(state string) bool {
	return slices.Contains(billingRecoveryUnconfirmedStates(), state)
}

func BillingRecoveryUsageCollecting(state string) bool {
	return state == BillingRecoveryStateUsageCollecting
}
