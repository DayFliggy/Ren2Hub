package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPricingAndModelVisibilityUseAccessibleScopedVersions(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withSelfUseModeDisabled(t)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)
	require.NoError(t, db.Create(&model.User{Id: 901, Username: "scoped-pricing-user", Group: "default", Status: common.UserStatusEnabled}).Error)
	for _, channelID := range []int{11, 12, 13, 14} {
		ratio := "2"
		if channelID == 14 {
			ratio = "1.000000000000000001"
		}
		require.NoError(t, db.Create(&model.Channel{Id: channelID, ChannelRatio: ratio, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}).Error)
		for _, name := range []string{"shared-price", "scoped-only", "private-price", "expired-price"} {
			require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: channelID, Enabled: true}).Error)
		}
	}
	require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: 901, ChannelID: 13, Source: model.RouteSourcePlatform, Status: model.RouteEntitlementStatusRevoked}).Error)
	publish := func(name string, channelID int, amount string, expires *time.Time) *billing.PriceVersion {
		t.Helper()
		scope := billing.PriceScopeGlobal
		if channelID > 0 {
			scope = billing.PriceScopeChannel
		}
		version, err := model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "Reviewed catalog fixture"}, Model: name, ScopeKind: scope, ChannelID: channelID, Mode: billing.ModeUnitPrice, EffectiveTo: expires,
			Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: amount}}}, 0, 1)
		require.NoError(t, err)
		return version
	}
	global := publish("shared-price", 0, "1", nil)
	scoped := publish("shared-price", 11, "2", nil)
	hidden := publish("shared-price", 13, "99", nil)
	publish("scoped-only", 11, "3", nil)
	publish("private-price", 13, "9", nil)
	expired := time.Now().Add(-time.Hour)
	_, expiredErr := model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "Expired fixture"}, Model: "expired-price", ScopeKind: billing.PriceScopeChannel, ChannelID: 11, Mode: billing.ModeUnitPrice, EffectiveTo: &expired, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "4"}}}, 0, 1)
	require.Error(t, expiredErr)

	public := func() (map[string]service.RoutePricing, string) {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/pricing", nil)
		ctx.Set("id", 901)
		GetPricing(ctx)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool                   `json:"success"`
			Data    []service.RoutePricing `json:"data"`
			Version string                 `json:"pricing_version"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success, recorder.Body.String())
		require.NotContains(t, recorder.Body.String(), "channel_ratio_min")
		require.NotContains(t, recorder.Body.String(), "channel_ratio_max")
		require.NotContains(t, recorder.Body.String(), "channel_id")
		require.NotContains(t, recorder.Body.String(), "provenance")
		result := make(map[string]service.RoutePricing)
		for _, row := range response.Data {
			result[row.ModelName] = row
		}
		return result, response.Version
	}
	prices, versionHash := public()
	require.Len(t, prices, 2)
	require.Contains(t, prices, "scoped-only")
	offers := prices["shared-price"].Offers
	require.Len(t, offers, 3, "the same base version with distinct adjustments must remain separate")
	require.Equal(t, scoped.ID, offers[0].PriceVersion.ID)
	require.Equal(t, "1", offers[0].Adjustments[0].Multiplier)
	require.Equal(t, "2", offers[0].DisplayPrices[0].Price)
	require.Equal(t, global.ID, offers[1].PriceVersion.ID)
	require.Equal(t, global.ID, offers[2].PriceVersion.ID)
	require.Equal(t, "2", offers[1].DisplayPrices[0].Price)
	require.Equal(t, "1.000000000000000001", offers[2].DisplayPrices[0].Price)
	require.NotEqual(t, offers[1].OfferID, offers[2].OfferID)
	require.Equal(t, "1", offers[1].PriceVersion.Prices[0].Price, "display projection must not mutate the frozen base")
	_, err := model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "Updated private retail price"}, Model: hidden.Model, ScopeKind: hidden.ScopeKind, ChannelID: hidden.ChannelID,
		Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "100"}}}, hidden.ID, 1)
	require.NoError(t, err)
	_, afterHiddenChange := public()
	require.Equal(t, versionHash, afterHiddenChange, "an inaccessible price must not change the caller's catalog version")

	list := func(tokenID int) map[string]struct{} {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		ctx.Set("id", 901)
		common.SetContextKey(ctx, constant.ContextKeyTokenId, tokenID)
		ListModels(ctx, constant.ChannelTypeOpenAI)
		return decodeListModelsResponse(t, recorder)
	}
	require.Equal(t, map[string]struct{}{"shared-price": {}, "scoped-only": {}}, list(0))
	groupID := 55
	require.NoError(t, db.Create(&model.UserRouteProfile{ID: 41, UserID: 901, TokenID: 777, Mode: model.RouteModeManual, ActiveGroupID: &groupID, Status: model.RouteProfileStatusEnabled}).Error)
	require.NoError(t, db.Create(&model.UserRouteGroup{ID: groupID, ProfileID: 41, Name: "visible channel", Kind: model.RouteGroupKindManual, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.UserRouteEntry{GroupID: groupID, ChannelID: 12, Source: model.RouteSourcePlatform, Enabled: true}).Error)
	require.Equal(t, map[string]struct{}{"shared-price": {}}, list(777), "a price on another channel cannot make an unpriced manual route visible")
}

func TestPublicPricingOfferPreservesRequestAlias(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	model.InvalidatePricingCache()
	t.Cleanup(model.InvalidatePricingCache)
	require.NoError(t, db.Create(&model.Channel{Id: 51, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, ChannelRatio: "0.5"}).Error)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "gpt-5-high", ChannelId: 51, Enabled: true}).Error)
	_, err := model.PublishBillingPrice(context.Background(), billing.PriceVersion{Model: "gpt-5", Mode: billing.ModeUnitPrice, Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "Reviewed alias price"}, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "0.123456789012345678"}}, Support: &billing.PricingSupport{Dimensions: []string{"p"}, ServiceTiers: []string{"default"}}}, 0, 1)
	require.NoError(t, err)
	rows, err := service.AvailableRoutePrices(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "gpt-5-high", rows[0].ModelName)
	require.Equal(t, "gpt-5", rows[0].Offers[0].PricedModel)
	require.Equal(t, "0.061728394506172839", rows[0].Offers[0].DisplayPrices[0].Price)
	require.Equal(t, []string{"default"}, rows[0].Offers[0].Conditions["service_tiers"])
}
