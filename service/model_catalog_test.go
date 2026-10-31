package service

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelCatalogSharesRoutingConstraintsAndPublicPrices(t *testing.T) {
	db := setupRouteTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "catalog-owner"}).Error)
	createRouteChannel(t, db, 1, "gpt-4o,hidden-model")
	createRouteChannel(t, db, 2, "gpt-4o")
	privateURL := "https://private-host.test"
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]any{"channel_ratio": "1.25", "base_url": privateURL, "key": "SECRET-never-public"}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2).Update("model_mapping", `{"a":1}`).Error)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 7).Updates(map[string]any{"model_limits_enabled": true, "model_limits": "gpt-4o"}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelRatio", Value: `{"gpt-4o":1.5}`}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "CompletionRatio", Value: `{"gpt-4o":4}`}).Error)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)
	view, err := BuildModelCatalog(context.Background(), 17, 7)
	require.NoError(t, err)
	require.Len(t, view.Models, 1)
	require.Equal(t, "gpt-4o", view.Models[0].ModelName)
	require.Equal(t, "configured", view.Models[0].PriceStatus)
	require.Len(t, view.Offers, 2)
	require.Len(t, view.Channels, 2)
	require.Equal(t, 2, view.Channels[1].ID)
	require.Equal(t, "invalid_mapping", view.Channels[1].Lab.Source, "an undecodable mapping marks its channel instead of failing the catalog")
	require.Equal(t, "3.75", view.Offers[0].PricingValues["input"])
	require.Equal(t, "15", view.Offers[0].PricingValues["output"])
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SECRET")
	require.NotContains(t, string(encoded), "private-host")
	require.NotContains(t, string(encoded), "hidden-model")
	var decoded struct {
		Models []map[string]any `json:"models"`
	}
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	require.Len(t, decoded.Models, 1)
	require.NotContains(t, decoded.Models[0], "capabilities", "the catalog has no capability source and must not publish a placeholder")
	_, err = BuildModelCatalog(context.Background(), 18, 7)
	require.Error(t, err)
	saveRouteTest(t) // Empty manual mode remains an explicit deny-all route.
	view, err = BuildModelCatalog(context.Background(), 17, 7)
	require.NoError(t, err)
	require.Empty(t, view.Models)
	require.Empty(t, view.Offers)
}

func TestModelCatalogSpecializedPricesKeepBillingIdentityAndScaleOnce(t *testing.T) {
	db := setupRouteTest(t)
	previousUnit := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previousUnit })
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "specialized-catalog-owner"}).Error)
	createRouteChannel(t, db, 1, "gemini-2.5-flash,claude-sonnet-4-20250514")
	// Provider mapping changes the upstream request model, not the catalog's
	// original billing identity or the native specialty price for that identity.
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Updates(map[string]any{
		"channel_ratio": "1.25",
		"model_mapping": `{"gemini-2.5-flash":"provider-audio","claude-sonnet-4-20250514":"provider-cache"}`,
	}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelRatio", Value: `{"gemini-2.5-flash":0.5,"claude-sonnet-4-20250514":1.5}`}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "AudioRatio", Value: `{"gemini-2.5-flash":1}`}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "CreateCacheRatio", Value: `{"claude-sonnet-4-20250514":1.25}`}).Error)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)
	view, err := BuildModelCatalog(context.Background(), 17, 7)
	require.NoError(t, err)
	require.Len(t, view.Models, 2)
	require.Len(t, view.Offers, 2)
	for _, item := range view.Models {
		switch item.ModelName {
		case "gemini-2.5-flash":
			require.Equal(t, "1", item.PricingValues["audio_input"])
		case "claude-sonnet-4-20250514":
			require.Equal(t, "3.75", item.PricingValues["cache_create"])
			require.Equal(t, "6", item.PricingValues["cache_create_1h"])
		}
	}
	for _, offer := range view.Offers {
		switch offer.ModelID {
		case "gemini-2.5-flash":
			require.Equal(t, "1.25", offer.PricingValues["audio_input"])
		case "claude-sonnet-4-20250514":
			require.Equal(t, "4.6875", offer.PricingValues["cache_create"])
			require.Equal(t, "7.5", offer.PricingValues["cache_create_1h"])
		}
	}
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"audio_input":"1"`)
	require.Contains(t, string(encoded), `"cache_create_1h":"7.5"`)
	require.Equal(t, 1, view.SchemaVersion)
}
