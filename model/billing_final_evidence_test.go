package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalConsumptionRequiresReplayableCompletedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BillingConsumption)
	}{
		{"missing usage", func(c *BillingConsumption) { c.Usage = nil }},
		{"missing charge", func(c *BillingConsumption) { c.Charge = nil }},
		{"partial", func(c *BillingConsumption) { c.Usage.Complete = false }},
		{"estimated", func(c *BillingConsumption) { c.Usage.Estimated = true }},
		{"invalid", func(c *BillingConsumption) { c.Usage.Invalid = true }},
		{"unknown source", func(c *BillingConsumption) { c.Usage.Source = "" }},
		{"unknown terminal", func(c *BillingConsumption) { c.Usage.Terminal = "" }},
		{"failed terminal", func(c *BillingConsumption) { c.Usage.Terminal = "failed" }},
		{"altered monetary total", func(c *BillingConsumption) { c.Charge.TotalUSD = "0.9" }},
		{"altered base detail", func(c *BillingConsumption) { c.Charge.BaseUSD = "0.9" }},
		{"different quantity with old charge", func(c *BillingConsumption) { c.Usage.Quantities["calls"] = "2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupBillingRecoveryTestDB(t)
			seedBillingAccount(t, db, 100, 100, false)
			newReservation(t, "final-guard")
			require.NoError(t, Reserve("final-guard", 40))
			consumption, err := bindConsumptionEvidence("final-guard", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30}})
			require.NoError(t, err)
			var before BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "final-guard").First(&before).Error)
			tc.mutate(&consumption)
			require.Error(t, PrepareBillingRecoveryConsumption("final-guard", consumption))
			var after BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "final-guard").First(&after).Error)
			assert.Equal(t, before.Version, after.Version)
			assert.Equal(t, BillingRecoveryStatusActive, after.Status)
			assert.Equal(t, BillingRecoveryStateUsagePending, after.ConsumptionState)
			assert.Equal(t, before.ConsumptionSnapshot, after.ConsumptionSnapshot, "invalid evidence cannot replace the pending attempt")
			assert.Empty(t, after.ChargeSnapshot)
			var user User
			require.NoError(t, db.First(&user, 1).Error)
			assert.EqualValues(t, 60, user.Quota)
			assert.EqualValues(t, 40, user.ReservedQuota)
		})
	}
}

func TestAmountOnlySettlementCannotBypassFinalEvidence(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "no-evidence")
	require.NoError(t, Reserve("no-evidence", 40))
	require.ErrorIs(t, UpdateBillingRecoverySettlement("no-evidence", -10), ErrBillingRecoveryStateConflict)
	// Defense in depth: even another writer setting pending cannot authorize
	// the monetary event without the complete immutable evidence.
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", "no-evidence").Updates(map[string]any{
		"status": BillingRecoveryStatusSettlementPending, "settlement_delta": -10,
	}).Error)
	require.Error(t, Settle("no-evidence", 30))
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 60, user.Quota)
	assert.EqualValues(t, 40, user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
	var events int64
	require.NoError(t, db.Model(&BillingEvent{}).Where("request_id = ? AND operation = ?", "no-evidence", "settle").Count(&events).Error)
	assert.Zero(t, events)
}

// Evidence is replayed when the settlement intent is prepared. The funds
// transaction only accepts the stored intent that matches its fingerprint and
// amount, so a later process or key rotation can still finish the request.
func TestFinalFundsTransactionReplaysStoredEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update map[string]any
	}{
		{"missing usage", map[string]any{"usage_snapshot": ""}},
		{"missing charge", map[string]any{"charge_snapshot": ""}},
		{"fingerprint mismatch", map[string]any{"consumption_fingerprint": "changed"}},
		{"tampered charge", map[string]any{"charge_snapshot": `{"final_quota":"30","total_usd":"1"}`}},
		{"tampered usage", map[string]any{"usage_snapshot": `{"source":"provider","complete":true,"terminal":"completed","quantities":{"calls":"2"}}`}},
		{"prepared intent after key rotation", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupBillingRecoveryTestDB(t)
			seedBillingAccount(t, db, 100, 100, false)
			newReservation(t, "stored-evidence")
			require.NoError(t, Reserve("stored-evidence", 40))
			consumption, err := bindConsumptionEvidence("stored-evidence", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30}})
			require.NoError(t, err)
			require.NoError(t, PrepareBillingRecoveryConsumption("stored-evidence", consumption))
			var user User
			if tc.update == nil {
				t.Setenv("CRYPTO_SECRET", "rotated-after-prepare")
				require.NoError(t, Settle("stored-evidence", 30))
				require.NoError(t, db.First(&user, 1).Error)
				assert.Equal(t, []common.Quota{70, 0, 30}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
				return
			}
			require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", "stored-evidence").Updates(tc.update).Error)
			require.Error(t, Settle("stored-evidence", 30))
			require.NoError(t, db.First(&user, 1).Error)
			assert.EqualValues(t, 40, user.ReservedQuota)
			assert.Zero(t, user.UsedQuota)
		})
	}
}

// A live session's cumulative snapshot can neither be released nor settled
// while it is collecting. It ends with the exact final usage, or with the last
// collected snapshot when the session cannot price a newer observation.
func TestLiveConsumptionKeepsUnknownUsageCollectingUntilFinalEvidence(t *testing.T) {
	for _, ending := range []string{"exact final usage", "last collected snapshot"} {
		t.Run(ending, func(t *testing.T) {
			db := setupBillingRecoveryTestDB(t)
			seedBillingAccount(t, db, 100, 100, false)
			newReservation(t, "live-final")
			require.NoError(t, Reserve("live-final", 40))
			consumption, err := bindConsumptionEvidence("live-final", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30}})
			require.NoError(t, err)
			consumption.Usage.Estimated, consumption.Usage.Complete = true, false
			var attempt BillingAttempt
			require.NoError(t, db.First(&attempt, "id = ?", consumption.AttemptID).Error)
			var quote billing.Quote
			require.NoError(t, common.UnmarshalJsonStr(attempt.Quote, &quote))
			usage, charge, err := EvaluateBilling(&quote, *consumption.Usage, billingexpr.RequestInput{}, "live-final", consumption.AttemptID, "settlement")
			require.NoError(t, err)
			consumption.Usage, consumption.Charge = &usage, charge
			require.NoError(t, RecordRealtimeBillingConsumption("live-final", consumption))
			var collecting BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "live-final").First(&collecting).Error)
			assert.Equal(t, BillingRecoveryStatusActive, collecting.Status)
			assert.Equal(t, BillingRecoveryStateUsageCollecting, collecting.ConsumptionState)
			assert.ErrorIs(t, MarkBillingRecoveryRefundPending("live-final", nil), ErrBillingRecoveryStateConflict)
			assert.ErrorIs(t, Settle("live-final", consumption.Charge.FinalQuota), ErrBillingRecoveryStateConflict)

			collected := ending == "last collected snapshot"
			if collected {
				require.NoError(t, FinalizeCollectedBillingConsumption("live-final"))
				require.ErrorIs(t, FinalizeCollectedBillingConsumption("live-final"), ErrNoCollectedBillingUsage)
			} else {
				consumption.Usage.Estimated, consumption.Usage.Complete = false, true
				usage, charge, err = EvaluateBilling(&quote, *consumption.Usage, billingexpr.RequestInput{}, "live-final", consumption.AttemptID, "settlement")
				require.NoError(t, err)
				consumption.Usage, consumption.Charge = &usage, charge
				require.NoError(t, PrepareBillingRecoveryConsumption("live-final", consumption))
				require.NoError(t, PrepareBillingRecoveryConsumption("live-final", consumption))
			}
			require.NoError(t, Settle("live-final", consumption.Charge.FinalQuota))
			require.NoError(t, Settle("live-final", consumption.Charge.FinalQuota))
			var user User
			require.NoError(t, db.First(&user, 1).Error)
			assert.Equal(t, common.Quota(70), user.Quota)
			assert.Zero(t, user.ReservedQuota)
			var settled BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "live-final").First(&settled).Error)
			var stored billing.Usage
			require.NoError(t, common.UnmarshalJsonStr(string(settled.UsageSnapshot), &stored))
			assert.Equal(t, collected, stored.Estimated, "the charged evidence is the one that ended the session")
		})
	}
}
