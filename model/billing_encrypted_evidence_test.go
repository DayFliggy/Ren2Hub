package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBillingSchemaRequiresStableExplicitEncryptionKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	t.Setenv("CRYPTO_SECRET", "")
	require.ErrorContains(t, prepareBillingSchema(db), "CRYPTO_SECRET")
	t.Setenv("CRYPTO_SECRET", "first-test-key")
	require.NoError(t, prepareBillingSchema(db))
	require.NoError(t, requireBillingSchema(db))
	var marker BillingSchema
	require.NoError(t, db.First(&marker, 1).Error)
	assert.Equal(t, 5, marker.Version)
	assert.NotEmpty(t, marker.EvidenceKeyID)
	assert.NotEqual(t, "first-test-key", marker.EvidenceKeyID)
	t.Setenv("CRYPTO_SECRET", "second-test-key")
	require.ErrorContains(t, requireBillingSchema(db), "does not match")
	t.Setenv("CRYPTO_SECRET", "first-test-key")
	require.NoError(t, requireBillingSchema(db))
}

func TestBillingConfigurationEvidenceIsImmutableAndDeduplicated(t *testing.T) {
	setupBillingRecoveryTestDB(t)
	version, err := billing.NewModelPriceVersion("proof", billing.PricingValues{"ModelPrice": "0.04"}, false)
	require.NoError(t, err)
	one, err := StoreBillingPriceEvidence(context.Background(), version)
	require.NoError(t, err)
	two, err := StoreBillingPriceEvidence(context.Background(), version)
	require.NoError(t, err)
	assert.Equal(t, one.ID, two.ID)
	version.Pricing["ModelPrice"] = "0.08"
	three, err := StoreBillingPriceEvidence(context.Background(), version)
	require.NoError(t, err)
	assert.NotEqual(t, one.ID, three.ID)
	quote, err := billing.NewQuote(*one, "1", nil, time.Now())
	require.NoError(t, err)
	require.NoError(t, ValidatePublishedBillingQuote(context.Background(), quote))
	require.Error(t, DB.Model(&BillingPriceVersion{}).Where("id = ?", one.ID).Update("hash", "altered").Error)
}

func TestBillingWrongKeyAndAttemptCannotSettleOrReleaseReservation(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "encrypted-request")
	require.NoError(t, Reserve("encrypted-request", 40))
	consumption, err := bindConsumptionEvidence("encrypted-request", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, ChannelId: 7, Quota: 30}})
	require.NoError(t, err)
	wrong := consumption
	wrong.AttemptID = "another-attempt"
	require.Error(t, PrepareBillingRecoveryConsumption("encrypted-request", wrong))
	// Evidence is authenticated when the settlement intent is prepared.
	t.Setenv("CRYPTO_SECRET", "wrong-test-key")
	require.Error(t, PrepareBillingRecoveryConsumption("encrypted-request", consumption))
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 40, user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
	t.Setenv("CRYPTO_SECRET", "billing-test-persistent-key")
	require.NoError(t, PrepareBillingRecoveryConsumption("encrypted-request", consumption))
	require.NoError(t, Settle("encrypted-request", 30))
	require.NoError(t, Settle("encrypted-request", 30))
	require.NoError(t, db.First(&user, 1).Error)
	assert.Zero(t, user.ReservedQuota)
	assert.EqualValues(t, 30, user.UsedQuota)
}

func TestBillingEstimateMustBeReplayableImmutableAndFunded(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "estimate-request")
	require.NoError(t, Reserve("estimate-request", 10))
	version, err := billing.NewModelPriceVersion("estimate", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `p*param("rate")`}, false)
	require.NoError(t, err)
	quote, err := publishedBillingTestQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	require.NoError(t, PrepareBillingAttempt(context.Background(), "estimate-request", "estimate-attempt", 7, quote))
	usage, charge, err := EvaluateBilling(quote, billing.Usage{Estimated: true, InputTokens: 10, InputIncludesCache: true}, billingexpr.RequestInput{Body: []byte(`{"rate":1,"secret":"private context"}`)}, "estimate-request", "estimate-attempt", "estimate")
	require.NoError(t, err)
	require.NoError(t, PersistBillingEstimate("estimate-attempt", usage, charge))
	require.NoError(t, PersistBillingEstimate("estimate-attempt", usage, charge))
	var attempt BillingAttempt
	require.NoError(t, db.First(&attempt, "id = ?", "estimate-attempt").Error)
	assert.NotContains(t, string(attempt.EstimateUsage), "private context")
	corrupt := *charge
	corrupt.FinalQuota++
	require.ErrorIs(t, PersistBillingEstimate("estimate-attempt", usage, &corrupt), ErrBillingEventConflict)
	var restored billing.Usage
	require.NoError(t, common.UnmarshalJsonStr(string(attempt.EstimateUsage), &restored))
	replayed, err := ReplayBilling(quote, restored, "estimate-request", "estimate-attempt", "estimate")
	require.NoError(t, err)
	assert.Equal(t, charge, replayed)
	_, err = ReplayBilling(quote, restored, "estimate-request", "estimate-attempt", "settlement")
	require.ErrorIs(t, err, billing.ErrEvaluationEvidence)
}
