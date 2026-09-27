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
