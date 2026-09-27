package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
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
			assert.Empty(t, after.ConsumptionSnapshot)
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

func TestFinalFundsTransactionReplaysStoredEvidence(t *testing.T) {
	for _, update := range []map[string]any{
		{"usage_snapshot": ""},
		{"charge_snapshot": ""},
		{"consumption_fingerprint": "changed"},
		{"charge_snapshot": `{"final_quota":"30","total_usd":"1"}`},
	} {
		db := setupBillingRecoveryTestDB(t)
		seedBillingAccount(t, db, 100, 100, false)
		newReservation(t, "stored-evidence")
		require.NoError(t, Reserve("stored-evidence", 40))
		consumption, err := bindConsumptionEvidence("stored-evidence", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30}})
		require.NoError(t, err)
		require.NoError(t, PrepareBillingRecoveryConsumption("stored-evidence", consumption))
		require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", "stored-evidence").Updates(update).Error)
		require.Error(t, Settle("stored-evidence", 30))
		var user User
		require.NoError(t, db.First(&user, 1).Error)
		assert.EqualValues(t, 40, user.ReservedQuota)
		assert.Zero(t, user.UsedQuota)
	}
}

func TestLiveConsumptionKeepsUnknownUsageCollectingUntilFinalEvidence(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "live-final")
	require.NoError(t, Reserve("live-final", 40))
	consumption, err := bindConsumptionEvidence("live-final", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30}})
	require.NoError(t, err)
	consumption.Usage.Estimated, consumption.Usage.Complete = true, false
	require.NoError(t, RecordRealtimeBillingConsumption("live-final", consumption))
	var collecting BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "live-final").First(&collecting).Error)
	assert.Equal(t, BillingRecoveryStatusActive, collecting.Status)
	assert.Equal(t, BillingRecoveryStateUsageCollecting, collecting.ConsumptionState)
	require.Error(t, PrepareBillingRecoveryConsumption("live-final", consumption))
	consumption.Usage.Estimated, consumption.Usage.Complete = false, true
	require.NoError(t, PrepareBillingRecoveryConsumption("live-final", consumption))
	require.NoError(t, PrepareBillingRecoveryConsumption("live-final", consumption))
	require.NoError(t, Settle("live-final", consumption.Charge.FinalQuota))
	require.NoError(t, Settle("live-final", consumption.Charge.FinalQuota))
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(70), user.Quota)
	assert.Zero(t, user.ReservedQuota)
}
