package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishedQuoteRequiresMatchingDatabaseVersion(t *testing.T) {
	setupBillingRecoveryTestDB(t)
	price, err := billing.NewModelPriceVersion("identity", billing.PricingValues{"ModelPrice": "0.000006"}, false)
	require.NoError(t, err)
	preview, err := billing.NewQuote(price, "1", nil, time.Unix(7200, 0))
	require.NoError(t, err)
	require.Error(t, ValidatePublishedBillingQuote(context.Background(), preview))
	quote, err := publishedBillingTestQuote(price, "1", nil, time.Unix(7200, 0))
	require.NoError(t, err)
	require.NoError(t, ValidatePublishedBillingQuote(context.Background(), quote))
	copyQuote := *quote
	copyQuote.PriceVersion.ID += 10000
	copyQuote.Hash = ""
	copyQuote.Hash, err = billing.Hash(copyQuote)
	require.NoError(t, err)
	require.ErrorContains(t, ValidatePublishedBillingQuote(context.Background(), &copyQuote), "published billing price is missing")
	copyQuote = *quote
	copyQuote.PriceVersion.Pricing = billing.PricingValues{"ModelPrice": "1"}
	definition := copyQuote.PriceVersion
	definition.ID, definition.CreatedAt, definition.Hash = 0, 0, ""
	copyQuote.PriceVersion.Hash, err = billing.Hash(definition)
	require.NoError(t, err)
	copyQuote.Hash = ""
	copyQuote.Hash, err = billing.Hash(copyQuote)
	require.NoError(t, err)
	require.ErrorIs(t, ValidatePublishedBillingQuote(context.Background(), &copyQuote), ErrBillingEventConflict)
	// A different active plan does not rewrite or invalidate the accepted one.
	price.Pricing["ModelPrice"] = "2"
	_, err = publishedBillingTestQuote(price, "1", nil, time.Unix(7201, 0))
	require.NoError(t, err)
	require.NoError(t, ValidatePublishedBillingQuote(context.Background(), quote))
}

func TestFinalConsumptionRequiresBoundImmutableAttempt(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "bound-attempt")
	require.NoError(t, Reserve("bound-attempt", 40))
	consumption, err := bindConsumptionEvidence("bound-attempt", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30, ChannelId: 7}})
	require.NoError(t, err)
	consumption.Log.ChannelId = 8
	require.ErrorContains(t, PrepareBillingRecoveryConsumption("bound-attempt", consumption), "frozen billing attempt is missing")
	consumption.Log.ChannelId = 7
	// The intent is replayed against its attempt when prepared; a damaged
	// stored attempt cannot back a settlement intent.
	require.NoError(t, db.Model(&BillingAttempt{}).Where("request_id = ?", "bound-attempt").Update("fingerprint", "changed").Error)
	require.ErrorIs(t, PrepareBillingRecoveryConsumption("bound-attempt", consumption), ErrBillingEventConflict)
	var recovery BillingRecovery
	require.NoError(t, db.First(&recovery, "request_id = ?", "bound-attempt").Error)
	assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
	assert.Empty(t, recovery.ConsumptionSnapshot)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, common.Quota(40), user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
}
