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
	require.NoError(t, db.AutoMigrate(&BillingPriceVersion{}, &BillingActivePrice{}))
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
			row := BillingPriceVersion{Model: version.Model, Definition: string(definition), Hash: hash, CreatedAt: time.Now().Unix()}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := tx.Create(&BillingActivePrice{Model: version.Model, VersionID: row.ID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
