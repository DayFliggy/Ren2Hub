package model

import (
	"context"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCatalogPriceUsesNativeUnitsAndOneChannelFactor(t *testing.T) {
	previous := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	base := catalogPrice("", PricingValues{"ModelRatio": float64(1.5), "CompletionRatio": float64(4)}, nil)
	require.Equal(t, map[string]string{"input": "3", "output": "12"}, base.PricingValues)
	scaled, err := ScaleCatalogPrice(base, "1.25")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"input": "3.75", "output": "15"}, scaled.PricingValues)
	require.Equal(t, "3", base.PricingValues["input"], "projection must not mutate base prices")
	zero := catalogPrice("", PricingValues{"ModelPrice": float64(0)}, nil)
	require.Equal(t, "zero", zero.PriceStatus)
	require.Equal(t, "0", zero.PricingValues["request"])
	require.Equal(t, "unavailable", catalogPrice("", PricingValues{}, nil).PriceStatus)
	expr := catalogPrice("", PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `tier("固定", p * 1.234567890123456789 + c * 0)`}, nil)
	require.Equal(t, "1.234567890123456789", expr.PricingValues["input"])
	dynamic := catalogPrice("", PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `p * param("rate")`}, nil)
	require.Equal(t, "unavailable", dynamic.PriceStatus)
	require.Equal(t, "expression_requires_usage_context", dynamic.PriceReason)
	require.Empty(t, dynamic.PricingValues)
}

func TestCatalogPriceProjectsNonTextDimensions(t *testing.T) {
	previous := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	price := catalogPrice("", PricingValues{
		"ModelRatio": float64(2), "CompletionRatio": float64(3), "CacheRatio": float64(0.5),
		"CreateCacheRatio": float64(1.25), "ImageRatio": float64(2), "AudioRatio": float64(0.75),
		"AudioCompletionRatio": float64(4),
	}, nil)
	require.Equal(t, map[string]string{
		"input": "4", "output": "12", "cache": "2", "cache_create": "5",
		"image": "8", "audio_input": "3", "audio_output": "12",
	}, price.PricingValues)
	scaled, err := ScaleCatalogPrice(price, "2")
	require.NoError(t, err)
	require.Equal(t, "24", scaled.PricingValues["audio_output"])
	require.Equal(t, "2", price.PricingValues["cache"], "scaling must not mutate the baseline")
}

func TestCatalogPriceClassifiesCustomCacheDimensionAsConfigured(t *testing.T) {
	previous := DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = db
	t.Cleanup(func() { DB = previous; connection, _ := db.DB(); _ = connection.Close() })
	require.NoError(t, db.AutoMigrate(&Option{}))
	require.NoError(t, db.Create(&Option{Key: "CacheRatio", Value: `{"gpt-4o":0.25}`}).Error)
	prices, err := CatalogPrices(context.Background(), []string{"gpt-4o"})
	require.NoError(t, err)
	require.Equal(t, "configured", prices["gpt-4o"].PriceStatus)
	require.Contains(t, prices["gpt-4o"].PricingValues, "cache")
}

func setupCatalogPricingTest(t *testing.T) *gorm.DB {
	t.Helper()
	previous, previousUnit := DB, common.QuotaPerUnit
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB, common.QuotaPerUnit = db, 500000
	t.Cleanup(func() {
		DB, common.QuotaPerUnit = previous, previousUnit
		connection, _ := db.DB()
		_ = connection.Close()
	})
	require.NoError(t, db.AutoMigrate(&Option{}))
	return db
}

func TestCatalogPriceNativeSpecializedDimensions(t *testing.T) {
	setupCatalogPricingTest(t)
	for _, tc := range []struct {
		unit             float64
		cache5m, cache1h string
	}{
		{500000, "3.75", "6"},
		{1000000, "1.875", "3"},
		{100000, "18.75", "30"},
	} {
		common.QuotaPerUnit = tc.unit
		prices, err := CatalogPrices(context.Background(), []string{"gemini-2.5-flash", "claude-sonnet-4-20250514", "gemini-2.5-flash-thinking-2048", "not-configured"})
		require.NoError(t, err)
		for _, name := range []string{"gemini-2.5-flash", "gemini-2.5-flash-thinking-2048"} {
			require.Equal(t, "default", prices[name].PriceStatus)
			require.Equal(t, "1", prices[name].PricingValues["audio_input"], "native Gemini's absolute audio price is independent of the quota unit")
			require.NotContains(t, prices[name].PricingValues, "audio_output", "no separate native output audio price is configured")
		}
		require.Equal(t, tc.cache5m, prices["claude-sonnet-4-20250514"].PricingValues["cache_create"])
		require.Equal(t, tc.cache1h, prices["claude-sonnet-4-20250514"].PricingValues["cache_create_1h"])
		require.Equal(t, "unavailable", prices["not-configured"].PriceStatus)
		require.Equal(t, "price_not_configured", prices["not-configured"].PriceReason)
		require.Empty(t, prices["not-configured"].PricingValues)
	}
}

func TestCatalogPriceProjectsOpenAITwelveHourCacheWritePrice(t *testing.T) {
	previous := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	values := PricingValues{"ModelRatio": float64(1.5), "CreateCacheRatio": float64(1.25)}
	price := catalogPrice("gpt-5.6-terra", values, values)
	require.Equal(t, "3.75", price.PricingValues["cache_create"])
	require.Equal(t, "7.5", price.PricingValues["cache_create_12h"])
}

func TestCatalogPriceSpecializedOverridesAndExplicitZero(t *testing.T) {
	db := setupCatalogPricingTest(t)
	require.NoError(t, db.Create(&Option{Key: "CreateCacheRatio", Value: `{"claude-sonnet-4-20250514":2,"claude-zero":0}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "ModelRatio", Value: `{"claude-sonnet-4-20250514":1.5,"claude-zero":1.5,"audio-zero":1,"audio-output-only":1,"gemini-2.5-flash":0}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "AudioRatio", Value: `{"audio-zero":0}`}).Error)
	require.NoError(t, db.Create(&Option{Key: "AudioCompletionRatio", Value: `{"audio-output-only":3}`}).Error)
	prices, err := CatalogPrices(context.Background(), []string{"claude-sonnet-4-20250514", "claude-zero", "audio-zero", "audio-output-only", "gemini-2.5-flash"})
	require.NoError(t, err)
	require.Equal(t, "configured", prices["claude-sonnet-4-20250514"].PriceStatus)
	require.Equal(t, "6", prices["claude-sonnet-4-20250514"].PricingValues["cache_create"])
	require.Equal(t, "9.6", prices["claude-sonnet-4-20250514"].PricingValues["cache_create_1h"])
	require.Equal(t, "0", prices["claude-zero"].PricingValues["cache_create"])
	require.Equal(t, "0", prices["claude-zero"].PricingValues["cache_create_1h"])
	require.Equal(t, "0", prices["audio-zero"].PricingValues["audio_input"])
	require.Equal(t, "0", prices["audio-zero"].PricingValues["audio_output"])
	require.Equal(t, "2", prices["audio-output-only"].PricingValues["audio_input"])
	require.Equal(t, "6", prices["audio-output-only"].PricingValues["audio_output"])
	require.Equal(t, "0", prices["gemini-2.5-flash"].PricingValues["input"])
	require.Equal(t, "1", prices["gemini-2.5-flash"].PricingValues["audio_input"])
	require.NotEqual(t, "zero", prices["gemini-2.5-flash"].PriceStatus, "free text does not make dedicated audio free")
}

func TestCatalogPriceDoesNotChooseBetweenConflictingAudioProtocols(t *testing.T) {
	db := setupCatalogPricingTest(t)
	require.NoError(t, db.Create(&Option{Key: "AudioRatio", Value: `{"gemini-2.5-flash":0}`}).Error)
	prices, err := CatalogPrices(context.Background(), []string{"gemini-2.5-flash"})
	require.NoError(t, err)
	require.Equal(t, "unavailable", prices["gemini-2.5-flash"].PriceStatus)
	require.Equal(t, "protocol_dependent_audio_price", prices["gemini-2.5-flash"].PriceReason)
	require.Empty(t, prices["gemini-2.5-flash"].PricingValues, "explicit free compatible audio must not hide the native $1 audio path")
}

func TestCatalogPriceRejectsOverflowingAudioDisplayPrice(t *testing.T) {
	setupCatalogPricingTest(t)
	price := catalogPrice("audio-overflow", PricingValues{
		"ModelRatio": math.MaxFloat64, "AudioRatio": math.MaxFloat64,
	}, nil)
	require.Equal(t, "unavailable", price.PriceStatus)
	require.Equal(t, "invalid_audio_price", price.PriceReason)
	require.Empty(t, price.PricingValues)
}
