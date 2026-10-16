package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMarketPreservesExactBaselinesAndMixedBillingWithoutPublishing(t *testing.T) {
	db, userID, _ := setupModelManifestTest(t)
	channel := model.Channel{Id: 101, Name: "Shared supply", Type: constant.ChannelTypeOpenAI,
		Status: common.ChannelStatusEnabled, Models: "market-text,market-call,market-free,market-dynamic,market-unpriced",
		ChannelRatio: "0.123456789012345678", Key: "market-secret-key"}
	require.NoError(t, db.Create(&channel).Error)
	for _, name := range []string{"market-text", "market-call", "market-free", "market-dynamic", "market-unpriced"} {
		require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Model: name, Enabled: true}).Error)
	}
	for key, value := range map[string]string{
		"ModelRatio":                   `{"market-text":0.123456789012345678}`,
		"ModelPrice":                   `{"market-call":0.004567890123456789,"market-free":0}`,
		"CacheRatio":                   `{"market-text":0}`,
		"billing_setting.billing_mode": `{"market-dynamic":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"market-dynamic":"tier(\"base\", p * 2 + c * 8)"}`,
	} {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	vendor := model.Vendor{Name: "Model maker", Status: 1}
	require.NoError(t, db.Create(&vendor).Error)
	toolCall := false
	require.NoError(t, db.Create(&model.Model{ModelName: "market-text", Description: "Text description", Icon: "OpenAI",
		VendorID: vendor.Id, Status: 1, Capabilities: &model.ModelCapabilities{ToolCall: &toolCall}}).Error)
	market, err := BuildModelMarket(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, market.Models, 4)
	require.Len(t, market.Channels, 1)
	require.Len(t, market.Offers, 10)
	assert.Equal(t, "0.123456789012345678", market.Channels[0].Ratio)
	byName := make(map[string]dto.MarketModel)
	for _, entry := range market.Models {
		byName[entry.ID] = entry
		count := 2
		if entry.ID == "market-call" || entry.ID == "market-free" {
			count = 3
		}
		require.Len(t, entry.Baselines, count)
		for _, price := range entry.Baselines {
			assert.True(t, price.Configured)
		}
	}
	assert.Equal(t, "0.123456789012345678", byName["market-text"].Baselines[0].Values["ModelRatio"], "the channel multiplier has not been applied to the baseline")
	assert.Equal(t, "0", byName["market-text"].Baselines[0].Values["CacheRatio"])
	assert.Equal(t, "0.004567890123456789", byName["market-call"].Baselines[0].Values["ModelPrice"])
	assert.Equal(t, "0", byName["market-free"].Baselines[0].Values["ModelPrice"])
	assert.Equal(t, `tier("base", p * 2 + c * 8)`, byName["market-dynamic"].Baselines[0].Values["billing_setting.billing_expr"])
	assert.Equal(t, "Model maker", byName["market-text"].Vendor)
	assert.Equal(t, "Text description", byName["market-text"].Description)
	require.NotNil(t, byName["market-text"].Capabilities)
	require.NotNil(t, byName["market-text"].Capabilities.ToolCall)
	assert.False(t, *byName["market-text"].Capabilities.ToolCall)
	assert.Nil(t, byName["market-call"].Capabilities)
	assert.Equal(t, dto.MarketTelemetry{Status: "unavailable", Reason: "channel_metrics_not_collected"}, market.Telemetry)
	encoded, err := common.Marshal(market)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"capabilities":null`)
	assert.NotContains(t, string(encoded), "market-secret-key")
	assert.NotContains(t, string(encoded), "base_url")
	var versions int64
	require.NoError(t, db.Model(&model.BillingPriceVersion{}).Count(&versions).Error)
	assert.Zero(t, versions, "reading market prices cannot publish billing evidence")

	require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("setting", `{"accept_unset_model_ratio_model":true}`).Error)
	market, err = BuildModelMarket(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, market.Models, 5)
	for _, entry := range market.Models {
		if entry.ID == "market-unpriced" {
			require.Len(t, entry.Baselines, 2)
			for _, price := range entry.Baselines {
				assert.False(t, price.Configured)
				assert.Empty(t, price.Values, "unconfigured is not a free model")
			}
		}
	}
}

func TestModelMarketRechecksChannelAccessAndModelVisibility(t *testing.T) {
	db, userID, _ := setupModelManifestTest(t)
	for index, name := range []string{"available", "revoked", "expired", "disabled", "hidden-card"} {
		seedManifestRoute(t, db, 201+index, name, constant.EndpointTypeOpenAI, model.RouteCapabilityStateEligible)
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id >= ?", 201).Update("channel_ratio", "1").Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"available":"0","revoked":"0","expired":"0","disabled":"0","hidden-card":"0"}`}).Error)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 202, Source: model.RouteSourcePlatform,
		Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}).Error)
	require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 203, Source: model.RouteSourcePlatform,
		Status: model.RouteEntitlementStatusEnabled, ExpiresAt: 1}).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 204).Update("status", common.ChannelStatusManuallyDisabled).Error)
	hidden := model.Model{ModelName: "hidden-card", Status: 0}
	require.NoError(t, hidden.Insert())
	market, err := BuildModelMarket(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, market.Models, 1)
	require.Len(t, market.Channels, 1)
	require.Len(t, market.Offers, 1)
	assert.Equal(t, "available", market.Models[0].ID)
	assert.Equal(t, 201, market.Offers[0].ChannelID)
	assert.Equal(t, map[string]dto.MarketEndpoint{
		string(constant.EndpointTypeOpenAI): {Path: "/v1/chat/completions", Method: "POST"},
	}, market.SupportedEndpoints, "endpoint documentation follows the same visible supply")

	require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 201, Source: model.RouteSourcePlatform,
		Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}).Error)
	market, err = BuildModelMarket(context.Background(), userID)
	require.NoError(t, err)
	assert.Empty(t, market.Models)
	assert.Empty(t, market.Channels)
	assert.Empty(t, market.Offers)
	assert.Empty(t, market.SupportedEndpoints)
	_, err = BuildModelMarket(context.Background(), 0)
	assert.ErrorIs(t, err, ErrRouteProfileForbidden)
}

func TestModelMarketPreservesCacheAndRequestPricingDetails(t *testing.T) {
	db, userID, _ := setupModelManifestTest(t)
	for index, name := range []string{"claude-market", "dall-e-3", "gpt-market-audio"} {
		endpoint := constant.EndpointTypeOpenAI
		if name == "dall-e-3" {
			endpoint = constant.EndpointTypeImageGeneration
		}
		seedManifestRoute(t, db, 401+index, name, endpoint, model.RouteCapabilityStateEligible)
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id >= ?", 401).Update("channel_ratio", "1").Error)
	for key, value := range map[string]string{
		"ModelRatio":           `{"claude-market":"1.5","gpt-market-audio":"0.123456789012345678"}`,
		"ModelPrice":           `{"dall-e-3":"0.04"}`,
		"CacheRatio":           `{"claude-market":"0.1"}`,
		"CreateCacheRatio":     `{"claude-market":"1.25"}`,
		"AudioRatio":           `{"gpt-market-audio":"0.234567890123456789"}`,
		"AudioCompletionRatio": `{"gpt-market-audio":"2.34567890123456789"}`,
	} {
		require.NoError(t, db.Create(&model.Option{Key: key, Value: value}).Error)
	}
	market, err := BuildModelMarket(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, market.Models, 3)
	for _, entry := range market.Models {
		require.Len(t, entry.Baselines, 1)
		price := entry.Baselines[0]
		switch entry.ID {
		case "claude-market":
			assert.Equal(t, "claude_ttl", price.CacheWriteMode)
			assert.Equal(t, "1.25", price.Values["CreateCacheRatio"])
		case "dall-e-3":
			assert.True(t, price.BillingDetails.ImageCount)
			require.NotEmpty(t, price.BillingDetails.RequestRules)
			for _, rule := range price.BillingDetails.RequestRules {
				assert.NotEmpty(t, rule.Condition)
				assert.NotEmpty(t, rule.Multiplier)
			}
		case "gpt-market-audio":
			assert.Equal(t, "0.234567890123456789", price.Values["AudioRatio"])
			assert.Equal(t, "2.34567890123456789", price.Values["AudioCompletionRatio"])
		}
	}
	encoded, err := common.Marshal(market)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "audio_input_price", "do not convert imprecise legacy float amounts back to exact-looking strings")
}

func TestModelMarketBindsPluginPricesToTheExecutingEndpoint(t *testing.T) {
	db, userID, _ := setupModelManifestTest(t)
	previous := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previous })
	const name = "market-shared-executor"
	for _, key := range []string{"market-first", "market-second"} {
		source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:%q,version:"1.0.0",author:{name:"Test"},upstreams:["new_api"],models:[%q],fetchMode:"per_task",usageSchema:{count:{type:"number",unit:"count"}},protocols:[{name:"openai_responses",supports:["sync","stream","background"]}]};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};export const protocols={openai_responses:{decodeRequest:function(){return {}},renderEvents:function(){return {events:[],done:false}},renderFinal:function(){return {}}}};`, key, key, name)
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	candidates := jsplugin.DefaultRegistry.Generation().LookupEndpointCandidates("POST", "/v1/responses", name)
	require.Len(t, candidates, 2)
	setting := `{"task_extend_plugin_keys":["market-first","market-second"]}`
	channel := model.Channel{Id: 301, Name: "Plugin supply", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled,
		Models: name, Key: "fixture-only", ChannelRatio: "0.2", Setting: &setting}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Model: name, Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"market-shared-executor":"0.04"}`}).Error)
	prices := map[string]string{
		billing_setting.PluginBillingExprKey(candidates[0].Plugin.Meta.Key, name): `u("count") * 0.01`,
		billing_setting.PluginBillingExprKey(candidates[1].Plugin.Meta.Key, name): `u("count") * 0.03`,
	}
	encoded, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: billing_setting.PluginBillingExprOption, Value: string(encoded)}).Error)
	market, err := BuildModelMarket(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, market.Models, 1)
	require.Len(t, market.Channels, 1)
	require.Len(t, market.Models[0].Baselines, 4)
	require.Len(t, market.Offers, 4)
	for _, price := range market.Models[0].Baselines {
		assert.NotContains(t, price.Values, billing_setting.PluginBillingExprOption)
		switch price.PluginKey {
		case "":
			assert.Equal(t, "0.04", price.Values["ModelPrice"])
			if price.BillingDetails.ImageCount {
				assert.Equal(t, []constant.EndpointType{constant.EndpointTypeImageGeneration}, price.SupportedEndpointTypes)
			} else {
				assert.Contains(t, price.SupportedEndpointTypes, constant.EndpointTypeOpenAI)
			}
			assert.NotContains(t, price.SupportedEndpointTypes, constant.EndpointTypeOpenAIResponse)
		case candidates[0].Plugin.Meta.Key:
			assert.Equal(t, `u("count") * 0.01`, price.Values["billing_setting.billing_expr"])
			assert.Contains(t, price.SupportedEndpointTypes, constant.EndpointTypeOpenAIResponse)
			assert.Contains(t, price.UsageSchema, "count")
		case candidates[1].Plugin.Meta.Key:
			assert.Equal(t, `u("count") * 0.03`, price.Values["billing_setting.billing_expr"])
			assert.NotContains(t, price.SupportedEndpointTypes, constant.EndpointTypeOpenAIResponse, "a cheaper/unselected plugin cannot claim an intercepted endpoint")
			assert.Contains(t, price.SupportedEndpointTypes, constant.EndpointTypeTaskPlugin)
		default:
			t.Fatalf("unexpected plugin %q", price.PluginKey)
		}
	}
	// One invalid executing override removes only that executable route. It
	// cannot fall back to native pricing or another plugin's expression.
	prices[billing_setting.PluginBillingExprKey(candidates[0].Plugin.Meta.Key, name)] = `u("undeclared") * 0.01`
	encoded, err = common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Option{}).Where(map[string]any{"key": billing_setting.PluginBillingExprOption}).Update("value", string(encoded)).Error)
	market, err = BuildModelMarket(context.Background(), userID)
	require.NoError(t, err)
	require.Len(t, market.Models, 1)
	assert.NotContains(t, market.Models[0].SupportedEndpointTypes, constant.EndpointTypeOpenAIResponse)
	require.Len(t, market.Models[0].Baselines, 3)
}

func TestModelMarketSeparatesNativeRequestAndImageUnits(t *testing.T) {
	for _, sample := range []struct {
		name, actual string
		imageRules   bool
	}{
		{name: "studio-alias", actual: "gpt-image-1"},
		{name: "dall-e-3", actual: "dall-e-3", imageRules: true},
	} {
		t.Run(sample.name, func(t *testing.T) {
			db, userID, _ := setupModelManifestTest(t)
			aliases := map[string]string{}
			if sample.name != sample.actual {
				aliases[sample.name] = sample.actual
			}
			mapping, err := common.Marshal(aliases)
			require.NoError(t, err)
			mappingText := string(mapping)
			endpoints, err := common.Marshal([]constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeImageGeneration})
			require.NoError(t, err)
			for _, channelID := range []int{501, 502} {
				channel := model.Channel{Id: channelID, Name: "Mixed native supply", Type: constant.ChannelTypeOpenAI,
					Models: sample.name, ModelMapping: &mappingText, ChannelRatio: "0.5", Status: common.ChannelStatusEnabled}
				require.NoError(t, db.Create(&channel).Error)
				ability := model.Ability{ChannelId: channelID, Model: sample.name, Enabled: true}
				require.NoError(t, db.Create(&ability).Error)
				fingerprint, err := channelCapabilityFingerprint(&channel, []model.Ability{ability})
				require.NoError(t, err)
				require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID,
					model.ChannelCapabilitySnapshotFence{}, fingerprint, modellab.DefaultCatalog().Version, []model.ChannelModelCapability{{
						RequestModel: sample.name, ActualModel: sample.actual, LabSlug: "custom", Source: "channel_configuration", Confidence: 1,
						EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
						ChannelType: constant.ChannelTypeOpenAI, State: model.RouteCapabilityStateEligible,
					}}))
			}
			prices, err := common.Marshal(map[string]string{sample.name: "0.04"})
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: string(prices)}).Error)
			market, err := BuildModelMarket(context.Background(), userID)
			require.NoError(t, err)
			require.Len(t, market.Models, 1)
			require.Len(t, market.Models[0].Baselines, 2, "same-price endpoints with different billing quantities must not collapse")
			require.Len(t, market.Offers, 4)
			byID := map[string]dto.MarketPricing{}
			for _, price := range market.Models[0].Baselines {
				byID[price.ID] = price
				assert.Equal(t, "0.04", price.Values["ModelPrice"])
				if price.BillingDetails.ImageCount {
					assert.Equal(t, []constant.EndpointType{constant.EndpointTypeImageGeneration}, price.SupportedEndpointTypes)
					assert.Equal(t, sample.imageRules, len(price.BillingDetails.RequestRules) > 0)
				} else {
					assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, price.SupportedEndpointTypes)
					assert.Empty(t, price.BillingDetails.RequestRules, "text requests do not inherit DALL-E image conditions")
				}
			}
			for _, offer := range market.Offers {
				baseline, exists := byID[offer.PricingID]
				require.True(t, exists)
				assert.Equal(t, baseline.SupportedEndpointTypes, offer.SupportedEndpointTypes)
			}
		})
	}
}

func TestModelMarketImagePluginKeepsTaskBillingUnits(t *testing.T) {
	db, userID, _ := setupModelManifestTest(t)
	previous := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previous })
	_, err := jsplugin.DefaultRegistry.Register(`
export const meta={apiVersion:1,key:"market-image-task",name:"Image task",version:"1.0.0",author:{name:"Test"},
  upstreams:["new_api"],models:["dall-e-3"],fetchMode:"per_task",usageSchema:{count:{type:"number",unit:"count"}},protocols:["openai_image"]};
export function buildSubmitRequest(){return {}}; export function parseSubmitResponse(){return {}};
export function buildQueryRequest(){return {}}; export function parseTaskResult(){return {}};
export const protocols={openai_image:{decodeRequest:function(){return {}},render:function(){return {data:[]}}}};`, jsplugin.Options{})
	require.NoError(t, err)
	setting := `{"task_extend_plugin_keys":["market-image-task"]}`
	channel := model.Channel{Id: 601, Name: "Image task supply", Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled,
		Models: "dall-e-3", ChannelRatio: "0.2", Setting: &setting}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channel.Id, Model: "dall-e-3", Enabled: true}).Error)
	require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"dall-e-3":"0.04"}`}).Error)
	for _, expression := range []bool{false, true} {
		if expression {
			value, err := common.Marshal(map[string]string{billing_setting.PluginBillingExprKey("market-image-task", "dall-e-3"): `u("count") * 0.04`})
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Option{Key: billing_setting.PluginBillingExprOption, Value: string(value)}).Error)
		}
		market, err := BuildModelMarket(context.Background(), userID)
		require.NoError(t, err)
		require.Len(t, market.Models, 1)
		var pluginPrices []dto.MarketPricing
		for _, price := range market.Models[0].Baselines {
			if price.PluginKey != "" {
				pluginPrices = append(pluginPrices, price)
			}
		}
		require.Len(t, pluginPrices, 1, "image and explicit task URLs share the executing plugin's own billing scope")
		price := pluginPrices[0]
		assert.Contains(t, price.SupportedEndpointTypes, constant.EndpointTypeImageGeneration)
		assert.Contains(t, price.SupportedEndpointTypes, constant.EndpointTypeTaskPlugin)
		assert.False(t, price.BillingDetails.ImageCount, "a protocol URL cannot add native image counting to a task plugin")
		assert.Empty(t, price.BillingDetails.RequestRules)
		if expression {
			assert.Equal(t, "tiered_expr", price.Values["billing_setting.billing_mode"])
			assert.Equal(t, `u("count") * 0.04`, price.Values["billing_setting.billing_expr"])
		} else {
			assert.Equal(t, "0.04", price.Values["ModelPrice"])
		}
	}
}

func TestModelMarketSeparatesNativeTextAndAudioBranches(t *testing.T) {
	for _, explicitAudio := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit_audio_%t", explicitAudio), func(t *testing.T) {
			db, userID, _ := setupModelManifestTest(t)
			const name = "market-mixed-audio"
			channel := model.Channel{Id: 701, Name: "Text and audio supply", Type: constant.ChannelTypeOpenAI,
				Models: name, ChannelRatio: "0.2", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			ability := model.Ability{ChannelId: channel.Id, Model: name, Enabled: true}
			require.NoError(t, db.Create(&ability).Error)
			endpoints, err := common.Marshal([]constant.EndpointType{
				constant.EndpointTypeOpenAI, constant.EndpointTypeAudio, constant.EndpointTypeRealtime,
			})
			require.NoError(t, err)
			fingerprint, err := channelCapabilityFingerprint(&channel, []model.Ability{ability})
			require.NoError(t, err)
			require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channel.Id,
				model.ChannelCapabilitySnapshotFence{}, fingerprint, modellab.DefaultCatalog().Version, []model.ChannelModelCapability{{
					RequestModel: name, ActualModel: name, LabSlug: "custom", Source: "channel_configuration", Confidence: 1,
					EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
					ChannelType: constant.ChannelTypeOpenAI, State: model.RouteCapabilityStateEligible,
				}}))
			prices := map[string]string{
				"ModelRatio": "1.5", "CompletionRatio": "2", "CacheRatio": "0.25", "CreateCacheRatio": "1.5", "ImageRatio": "2",
			}
			if explicitAudio {
				prices["AudioRatio"] = "3"
				prices["AudioCompletionRatio"] = "4"
			}
			for key, price := range prices {
				value, err := common.Marshal(map[string]string{name: price})
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.Option{Key: key, Value: string(value)}).Error)
			}
			market, err := BuildModelMarket(context.Background(), userID)
			require.NoError(t, err)
			require.Len(t, market.Models, 1)
			require.Len(t, market.Models[0].Baselines, 2, "text and audio may share configuration but not applicable dimensions")
			require.Len(t, market.Offers, 2)
			byID := map[string]dto.MarketPricing{}
			for _, price := range market.Models[0].Baselines {
				byID[price.ID] = price
				assert.Equal(t, "1.5", price.Values["ModelRatio"])
				assert.Equal(t, "0.25", price.Values["CacheRatio"], "raw configuration is preserved; endpoint scope determines its application")
				if len(price.SupportedEndpointTypes) == 1 {
					assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAI}, price.SupportedEndpointTypes)
					assert.Equal(t, explicitAudio, price.BillingDetails.AudioTextBranches)
				} else {
					assert.Equal(t, []constant.EndpointType{constant.EndpointTypeAudio, constant.EndpointTypeRealtime}, price.SupportedEndpointTypes)
					assert.True(t, price.BillingDetails.AudioTextBranches)
				}
			}
			for _, offer := range market.Offers {
				baseline, exists := byID[offer.PricingID]
				require.True(t, exists)
				assert.Equal(t, baseline.SupportedEndpointTypes, offer.SupportedEndpointTypes)
			}
		})
	}
}
