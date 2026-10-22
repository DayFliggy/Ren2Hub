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
