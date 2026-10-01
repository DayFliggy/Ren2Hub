package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBillingSchemaRejectsRetiredRecoveryStatesWithoutChangingFunds(t *testing.T) {
	for _, test := range []struct{ status, consumption string }{
		{"reconciliation_required", BillingRecoveryStateUsagePending},
		{"reconciliation_required", BillingRecoveryStateTaskPending},
		{BillingRecoveryStatusActive, "pricing_pending"},
		{BillingRecoveryStatusActive, "pricing_collect"},
		{BillingRecoveryStatusSettled, BillingRecoveryStateUsagePending},
		{BillingRecoveryStatusSettled, BillingRecoveryStateUsageCollecting},
		{BillingRecoveryStatusSettled, BillingRecoveryStateUnprepared},
		{BillingRecoveryStatusRefunded, "pricing_pending"},
	} {
		t.Run(test.status+"/"+test.consumption, func(t *testing.T) {
			db := setupBillingRecoveryTestDB(t)
			seedBillingAccount(t, db, 100, 100, false)
			newReservation(t, "legacy-schema-five")
			require.NoError(t, Reserve("legacy-schema-five", 40))
			require.NoError(t, db.AutoMigrate(&BillingSchema{}))
			cipher, err := billingEvidenceCipher()
			require.NoError(t, err)
			require.NoError(t, db.Create(&BillingSchema{ID: 1, Version: 5, EvidenceKeyID: cipher.KeyID()}).Error)
			require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", "legacy-schema-five").Updates(map[string]any{
				"status": test.status, "consumption_state": test.consumption,
			}).Error)
			var before BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "legacy-schema-five").First(&before).Error)
			require.ErrorContains(t, prepareBillingSchema(db), "unsupported billing recovery states")
			require.ErrorContains(t, requireBillingSchema(db), "unsupported billing recovery states")
			var after BillingRecovery
			var user User
			var token Token
			require.NoError(t, db.First(&after, before.ID).Error)
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&token, 1).Error)
			assert.Equal(t, before, after)
			assert.EqualValues(t, 60, user.Quota)
			assert.EqualValues(t, 40, user.ReservedQuota)
			assert.Zero(t, user.UsedQuota)
			assert.EqualValues(t, 60, token.RemainQuota)
			assert.EqualValues(t, 40, token.ReservedQuota)
			assert.Zero(t, token.UsedQuota)
			var events int64
			require.NoError(t, db.Model(&BillingEvent{}).Count(&events).Error)
			assert.EqualValues(t, 1, events, "startup must not invent a settlement or release")
		})
	}
}

func TestBillingSchemaAllowsSupportedRecoveryStates(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&BillingSchema{}))
	cipher, err := billingEvidenceCipher()
	require.NoError(t, err)
	require.NoError(t, db.Create(&BillingSchema{ID: 1, Version: 5, EvidenceKeyID: cipher.KeyID()}).Error)
	for _, test := range []struct{ status, consumption string }{
		{BillingRecoveryStatusActive, ""},
		{BillingRecoveryStatusActive, BillingRecoveryStateUsagePending},
		{BillingRecoveryStatusActive, BillingRecoveryStateUsageCollecting},
		{BillingRecoveryStatusActive, BillingRecoveryStateTaskPending},
		{BillingRecoveryStatusActive, BillingRecoveryStateUnprepared},
		{BillingRecoveryStatusSettlementPending, BillingRecoveryStatePending},
		{BillingRecoveryStatusRefundPending, BillingRecoveryStatePending},
		{BillingRecoveryStatusSettled, BillingRecoveryStatePending},
		{BillingRecoveryStatusSettled, BillingRecoveryStateCompleted},
		{BillingRecoveryStatusRefunded, BillingRecoveryStatePending},
		{BillingRecoveryStatusRefunded, BillingRecoveryStateCompleted},
	} {
		require.NoError(t, db.Create(&BillingRecovery{RequestID: test.status + "/" + test.consumption, Status: test.status, ConsumptionState: test.consumption, Source: "wallet"}).Error)
	}
	require.NoError(t, prepareBillingSchema(db))
	require.NoError(t, requireBillingSchema(db))
}
