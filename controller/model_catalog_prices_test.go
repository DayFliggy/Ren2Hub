package controller

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/require"
)

func TestCatalogSpecializedPricesAfterEditorSave(t *testing.T) {
	previousUnit := common.QuotaPerUnit
	t.Cleanup(func() { common.QuotaPerUnit = previousUnit })
	for _, tc := range []struct {
		unit                       float64
		cache5m, cache1h, cache12h string
	}{
		{500000, "6", "9.6", "12"},
		{1000000, "3", "4.8", "6"},
	} {
		t.Run(fmt.Sprintf("quota_unit_%.0f", tc.unit), func(t *testing.T) {
			modelManagementDB(t, "sqlite", "")
			common.QuotaPerUnit = tc.unit
			for _, entry := range []struct {
				name    string
				pricing model.PricingValues
			}{
				{"claude-sonnet-4-20250514", model.PricingValues{"ModelRatio": 1.5, "CreateCacheRatio": 2.0}},
				{"gpt-5.6-terra", model.PricingValues{"ModelRatio": 1.5, "CreateCacheRatio": 2.0}},
				{"gemini-2.5-flash", model.PricingValues{"ModelRatio": 0.0}},
			} {
				t.Run(entry.name, func(t *testing.T) {
					var before struct {
						Success bool
						Data    model.ModelPricingSnapshot
					}
					modelManagementRequest(t, GetModelPricingConfig, http.MethodGet, "/api/option/model_pricing?model="+entry.name, nil, &before)
					require.True(t, before.Success)
					require.Len(t, before.Data.Entries, 1)
					var preview struct {
						Success bool
						Data    model.ModelPricingDescription
					}
					modelManagementRequest(t, PreviewModelPricing, http.MethodPost, "/api/option/model_pricing/preview", map[string]any{"model_name": entry.name, "pricing": entry.pricing}, &preview)
					require.True(t, preview.Success)
					var saved struct{ Success bool }
					recorder := modelManagementRequest(t, UpdateModelPricingConfig, http.MethodPut, "/api/option/model_pricing", map[string]any{"changes": []model.ModelPricingChange{{ModelName: entry.name, ExpectedVersion: before.Data.Entries[0].Version, Pricing: entry.pricing}}}, &saved)
					require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					require.True(t, saved.Success, recorder.Body.String())
					var reread struct {
						Success bool
						Data    model.ModelPricingSnapshot
					}
					modelManagementRequest(t, GetModelPricingConfig, http.MethodGet, "/api/option/model_pricing?model="+entry.name, nil, &reread)
					require.True(t, reread.Success)
					require.Len(t, reread.Data.Entries, 1)
					require.Equal(t, entry.pricing, reread.Data.Entries[0].Configured)
					require.Equal(t, preview.Data, reread.Data.Entries[0].ModelPricingDescription)
					prices, err := model.CatalogPrices(context.Background(), []string{entry.name})
					require.NoError(t, err)
					require.Equal(t, "configured", prices[entry.name].PriceStatus)
					if entry.name == "gemini-2.5-flash" {
						require.Equal(t, "0", prices[entry.name].PricingValues["input"])
						require.Equal(t, "1", prices[entry.name].PricingValues["audio_input"])
						require.NotNil(t, preview.Data.BillingDetails.AudioInputPrice)
						require.Equal(t, 1.0, *preview.Data.BillingDetails.AudioInputPrice)
						return
					}
					if entry.name == "gpt-5.6-terra" {
						require.Equal(t, model.CacheWriteOpenAITTL, preview.Data.CacheWriteMode)
						require.Equal(t, tc.cache5m, prices[entry.name].PricingValues["cache_create"])
						require.Equal(t, tc.cache12h, prices[entry.name].PricingValues["cache_create_12h"])
						conversion, err := model.PreviewModelPricingConversion(entry.name, entry.pricing)
						require.NoError(t, err)
						convertedPrices, ok := billingexpr.StaticReferencePrices(conversion.Expression)
						require.True(t, ok)
						require.Equal(t, prices[entry.name].PricingValues["cache_create_12h"], convertedPrices["cache_create_12h"])
						return
					}
					require.Equal(t, model.CacheWriteClaudeTTL, preview.Data.CacheWriteMode)
					require.Equal(t, tc.cache5m, prices[entry.name].PricingValues["cache_create"])
					require.Equal(t, tc.cache1h, prices[entry.name].PricingValues["cache_create_1h"])
					conversion, err := model.PreviewModelPricingConversion(entry.name, entry.pricing)
					require.NoError(t, err)
					require.Empty(t, conversion.UnsupportedReason)
					convertedPrices, ok := billingexpr.StaticReferencePrices(conversion.Expression)
					require.True(t, ok)
					require.Equal(t, prices[entry.name].PricingValues["cache_create"], convertedPrices["cache_create"])
					require.Equal(t, prices[entry.name].PricingValues["cache_create_1h"], convertedPrices["cache_create_1h"])
				})
			}
		})
	}
}
