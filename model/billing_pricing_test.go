package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupBillingPricingDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&BillingPriceVersion{}, &BillingActivePrice{}, &BillingPricingSource{}, &BillingPricingSnapshot{}))
	previous := DB
	mainType, logType := common.MainDatabaseType(), common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previous
		common.SetDatabaseTypes(mainType, logType)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestPublishedPricesUseCanonicalDecimalAPIStrings(t *testing.T) {
	setupBillingPricingDB(t)
	for _, test := range []struct{ input, expected string }{{"1e-6", "0.000001"}, {"01.20", "1.2"}, {"+1.25", "1.25"}, {".1", "0.1"}} {
		t.Run(test.input, func(t *testing.T) {
			version, err := PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: test.input, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: test.input}}}, 0, 1)
			require.NoError(t, err)
			assert.Equal(t, test.expected, version.Prices[0].Price)
			restored, err := GetActiveBillingPrice(context.Background(), test.input)
			require.NoError(t, err)
			assert.Equal(t, version, restored)
		})
	}
}

func TestPublishedBillingPriceVersionsAreImmutableAndConflictChecked(t *testing.T) {
	db := setupBillingPricingDB(t)
	ctx := context.Background()
	version := billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "model", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}}}
	first, err := PublishBillingPrice(ctx, version, 0, 1)
	require.NoError(t, err)
	require.Positive(t, first.ID)
	version.Prices[0].Price = "3"
	_, err = PublishBillingPrice(ctx, version, 0, 1)
	require.ErrorIs(t, err, ErrBillingPriceConflict)
	second, err := PublishBillingPrice(ctx, version, first.ID, 1)
	require.NoError(t, err)
	assert.Greater(t, second.ID, first.ID)
	active, err := GetActiveBillingPrice(ctx, "model")
	require.NoError(t, err)
	assert.Equal(t, second, active)
	var original BillingPriceVersion
	require.NoError(t, db.First(&original, first.ID).Error)
	decoded, err := decodeBillingPrice(original)
	require.NoError(t, err)
	assert.Equal(t, "2", decoded.Prices[0].Price)
	require.Error(t, db.Model(&original).Update("definition", "{}").Error)
	require.Error(t, db.Delete(&original).Error)
	_, err = GetActiveBillingPrice(ctx, "model-unknown")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestChannelScopedPricePrecedesExplicitGlobalFallback(t *testing.T) {
	setupBillingPricingDB(t)
	ctx := context.Background()
	global, err := PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		Model: "scoped-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}},
	}, 0, 1)
	require.NoError(t, err)
	channel, err := PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		ScopeKind: billing.PriceScopeChannel, ChannelID: 42,
		Model: "scoped-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "3"}},
	}, 0, 1)
	require.NoError(t, err)

	selected, err := GetActiveBillingPriceForChannel(ctx, "scoped-model", 42)
	require.NoError(t, err)
	assert.Equal(t, channel.ID, selected.ID)
	assert.Equal(t, billing.PriceScopeChannel, selected.ScopeKind)
	assert.Equal(t, 42, selected.ChannelID)

	fallback, err := GetActiveBillingPriceForChannel(ctx, "scoped-model", 99)
	require.NoError(t, err)
	assert.Equal(t, global.ID, fallback.ID)
	assert.Equal(t, billing.PriceScopeGlobal, fallback.ScopeKind)
}

func TestUnavailableChannelScopeDoesNotFallBackToGlobal(t *testing.T) {
	db := setupBillingPricingDB(t)
	ctx := context.Background()
	global, err := PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		Model: "blocked-channel-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}},
	}, 0, 1)
	require.NoError(t, err)
	require.NoError(t, db.Create(&BillingActivePrice{Model: global.Model, ChannelID: 42, Origin: BillingPriceOriginUnavailable}).Error)

	_, err = GetActiveBillingPriceForChannel(ctx, global.Model, 42)
	assert.ErrorIs(t, err, ErrAutomaticBillingPriceUnavailable)
}

func TestAutomaticPublicationRejectsChannelScope(t *testing.T) {
	setupBillingPricingDB(t)
	source, entries := installPricingSnapshot(t, "sub2api", `{"automatic-channel-model":{"input_cost_per_token":0.000001,"output_cost_per_token":0}}`)
	draft := entries["automatic-channel-model"].Price
	draft.ScopeKind = billing.PriceScopeChannel
	draft.ChannelID = 42
	_, err := PublishAutomaticBillingPrice(context.Background(), draft, "sub2api", draft.Model, source.Hash)
	assert.ErrorContains(t, err, "automatic billing prices must use global model scope")
}

func TestManualPublicationKeepsIndependentChannelPointers(t *testing.T) {
	setupBillingPricingDB(t)
	ctx := context.Background()
	global, err := PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		Model: "manual-scope-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}},
	}, 0, 1)
	require.NoError(t, err)
	channel, err := PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		ScopeKind: billing.PriceScopeChannel, ChannelID: 17, Model: "manual-scope-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}},
	}, 0, 1)
	require.NoError(t, err)
	updated, err := PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		ScopeKind: billing.PriceScopeChannel, ChannelID: 17, Model: "manual-scope-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "3"}},
	}, channel.ID, 1)
	require.NoError(t, err)
	assert.NotEqual(t, global.ID, updated.ID)
	selected, err := GetActiveBillingPriceForChannel(ctx, "manual-scope-model", 17)
	require.NoError(t, err)
	assert.Equal(t, updated.ID, selected.ID)
	fallback, err := GetActiveBillingPriceForChannel(ctx, "manual-scope-model", 18)
	require.NoError(t, err)
	assert.Equal(t, global.ID, fallback.ID)
}

func TestPublishBillingPriceCanonicalizesTierFlatFeeAndWindow(t *testing.T) {
	setupBillingPricingDB(t)
	ctx := context.Background()
	from := time.Now().UTC().Add(-time.Hour)
	to := from.Add(24 * time.Hour)
	version, err := PublishBillingPrice(ctx, billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"},
		Model: "tiered-model", Mode: billing.ModeUnitPrice, FlatFee: "01.20",
		EffectiveFrom: &from, EffectiveTo: &to,
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", TierMode: billing.TierModeGraduated, Tiers: []billing.PriceTier{
			{UpTo: "10", UnitPrice: "0.0100"}, {UnitPrice: "0.005"},
		}}},
	}, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, "1.2", version.FlatFee)
	assert.Equal(t, "10", version.Prices[0].Tiers[0].UpTo)
	assert.Equal(t, "0.01", version.Prices[0].Tiers[0].UnitPrice)
	assert.Equal(t, "0.005", version.Prices[0].Tiers[1].UnitPrice)
	loaded, err := GetActiveBillingPrice(ctx, "tiered-model")
	require.NoError(t, err)
	assert.Equal(t, version, loaded)
}

func TestManualPriceRequiresEvidenceAndCurrentWindow(t *testing.T) {
	setupBillingPricingDB(t)
	ctx := context.Background()
	draft := billing.PriceVersion{Model: "proof", Mode: billing.ModeFree}
	_, err := PublishBillingPrice(ctx, draft, 0, 1)
	require.ErrorContains(t, err, "retail explanation")
	draft.Provenance = &billing.PriceProvenance{Kind: "retail", RetailReason: "promotional free model"}
	future := time.Now().Add(time.Hour)
	draft.EffectiveFrom = &future
	_, err = PublishBillingPrice(ctx, draft, 0, 1)
	require.ErrorContains(t, err, "future")
	draft.EffectiveFrom = nil
	expired := time.Now().Add(-time.Hour)
	draft.EffectiveTo = &expired
	_, err = PublishBillingPrice(ctx, draft, 0, 1)
	require.ErrorContains(t, err, "expired")
	draft.EffectiveTo = nil
	draft.Origin = "forged"
	draft.SourceHash = "forged"
	version, err := PublishBillingPrice(ctx, draft, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, BillingPriceOriginManual, version.Origin)
	assert.Empty(t, version.SourceHash)
}
