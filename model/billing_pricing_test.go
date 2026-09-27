package model

import (
	"context"
	"strings"
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
	require.NoError(t, db.AutoMigrate(&BillingPriceVersion{}, &BillingActivePrice{}, &BillingPricingSource{}))
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
			version, err := PublishBillingPrice(context.Background(), billing.PriceVersion{Model: test.input, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: test.input}}}, 0, 1)
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
	version := billing.PriceVersion{Model: "model", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}}}
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
	global, err := PublishBillingPrice(ctx, billing.PriceVersion{
		Model: "scoped-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}},
	}, 0, 1)
	require.NoError(t, err)
	channel, err := PublishBillingPrice(ctx, billing.PriceVersion{
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
	global, err := PublishBillingPrice(ctx, billing.PriceVersion{
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
	ctx := context.Background()
	hash := strings.Repeat("a", 64)
	require.NoError(t, SaveBillingPricingSource(ctx, &BillingPricingSource{Source: BillingPriceOriginSub2API, Document: `{}`, Hash: hash}))
	_, err := PublishAutomaticBillingPrice(ctx, billing.PriceVersion{
		ScopeKind: billing.PriceScopeChannel, ChannelID: 42, Model: "automatic-channel-model",
		Mode:   billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}},
	}, BillingPriceOriginSub2API, "automatic-channel-model", hash)
	assert.ErrorContains(t, err, "automatic billing prices must use global model scope")
}

func TestManualPublicationKeepsIndependentChannelPointers(t *testing.T) {
	setupBillingPricingDB(t)
	ctx := context.Background()
	global, err := PublishBillingPrice(ctx, billing.PriceVersion{
		Model: "manual-scope-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}},
	}, 0, 1)
	require.NoError(t, err)
	channel, err := PublishBillingPrice(ctx, billing.PriceVersion{
		ScopeKind: billing.PriceScopeChannel, ChannelID: 17, Model: "manual-scope-model", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}},
	}, 0, 1)
	require.NoError(t, err)
	updated, err := PublishBillingPrice(ctx, billing.PriceVersion{
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
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	version, err := PublishBillingPrice(ctx, billing.PriceVersion{
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

func TestPublishedPriceFreezesAuthoritativeProvenance(t *testing.T) {
	setupBillingPricingDB(t)
	ctx := context.Background()
	hash := strings.Repeat("b", 64)
	require.NoError(t, SaveBillingPricingSource(ctx, &BillingPricingSource{Source: BillingPriceOriginAxonHub, Document: `{}`, Hash: hash}))
	manual, err := PublishBillingPrice(ctx, billing.PriceVersion{
		Model: "provenance-model", Mode: billing.ModeUnitPrice,
		Origin: "forged", SourceModel: "forged-provider", SourceHash: "invalid",
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}},
	}, 0, 1)
	require.NoError(t, err)
	assert.Equal(t, BillingPriceOriginManual, manual.Origin)
	assert.Empty(t, manual.SourceModel)
	assert.Empty(t, manual.SourceHash)
	quote, err := billing.NewQuote(*manual, "1", nil, time.Now())
	require.NoError(t, err)
	restored, err := RestoreAutomaticBillingPrice(ctx, *manual, BillingPriceOriginAxonHub, "provider/provenance-model", hash, manual.ID)
	require.NoError(t, err)
	assert.NotEqual(t, manual.ID, restored.ID)
	assert.NotEqual(t, manual.Hash, restored.Hash)
	assert.Equal(t, BillingPriceOriginAxonHub, restored.Origin)
	assert.Equal(t, "provider/provenance-model", restored.SourceModel)
	assert.Equal(t, hash, restored.SourceHash)
	assert.Equal(t, BillingPriceOriginManual, quote.PriceVersion.Origin)
	assert.Empty(t, quote.PriceVersion.SourceHash)
	loaded, err := GetActiveBillingPrice(ctx, manual.Model)
	require.NoError(t, err)
	assert.Equal(t, restored, loaded)
}

func TestInitialBillingCatalogIsAtomicAndRestartSafe(t *testing.T) {
	db := setupBillingPricingDB(t)
	ctx := context.Background()
	versions, unconfigured, err := InitialBillingPriceCatalog()
	require.NoError(t, err)
	require.NotEmpty(t, versions)
	assert.Contains(t, unconfigured, "dall-e-3")
	for _, version := range versions {
		require.NoError(t, billing.ValidatePriceVersion(version), version.Model)
	}
	require.NoError(t, SeedBillingPriceVersions(ctx))
	require.NoError(t, SeedBillingPriceVersions(ctx))
	active, err := ListActiveBillingPrices(ctx)
	require.NoError(t, err)
	assert.Len(t, active, len(versions))
	var count int64
	require.NoError(t, db.Model(&BillingPriceVersion{}).Count(&count).Error)
	assert.Equal(t, int64(len(versions)), count)
	price, err := GetActiveBillingPrice(ctx, "gpt-4o")
	require.NoError(t, err)
	assert.Equal(t, "2.5", func() string {
		for _, p := range price.Prices {
			if p.Quantity == "p" {
				return p.Price
			}
		}
		return ""
	}())
}

// SeedBillingPriceVersions recreates the retired installer only for legacy
// compatibility tests. Production installs use the automatic source catalogs.
func SeedBillingPriceVersions(ctx context.Context) error {
	versions, _, err := InitialBillingPriceCatalog()
	if err != nil {
		return err
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&BillingPriceVersion{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		for _, version := range versions {
			if err := billing.ValidateFrozenPriceVersion(version); err != nil {
				return err
			}
			definition, err := common.Marshal(version)
			if err != nil {
				return err
			}
			hash, err := billing.Hash(version)
			if err != nil {
				return err
			}
			row := BillingPriceVersion{ScopeKind: version.ScopeKind, ChannelID: version.ChannelID, Model: version.Model, Definition: string(definition), Hash: hash, CreatedAt: time.Now().Unix()}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := tx.Create(&BillingActivePrice{Model: version.Model, ChannelID: version.ChannelID, VersionID: row.ID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
