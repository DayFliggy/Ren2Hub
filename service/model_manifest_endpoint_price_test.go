package service

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelManifestEndpointPricingMatchesExecutingProtocol(t *testing.T) {
	for _, tc := range []struct {
		name                                    string
		nativePrice, pluginPrice, invalidPlugin bool
		wantChat, wantResponses                 bool
	}{
		{name: "neither"},
		{name: "plugin only", pluginPrice: true, wantResponses: true},
		// The existing task quote resolver permits a configured per-call price.
		{name: "native per-call fallback", nativePrice: true, wantChat: true, wantResponses: true},
		{name: "both", nativePrice: true, pluginPrice: true, wantChat: true, wantResponses: true},
		{name: "invalid plugin cannot fall back to native", nativePrice: true, invalidPlugin: true, wantChat: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, userID, tokenID := setupModelManifestTest(t)
			previous := jsplugin.DefaultRegistry
			jsplugin.DefaultRegistry = jsplugin.NewRegistry()
			t.Cleanup(func() { jsplugin.DefaultRegistry = previous })
			const name, key = "manifest-endpoint-model", "manifest-endpoint-provider"
			source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:"Fixture",version:"1.0.0",author:{name:"Test"},channelTypes:[1],models:[%q,"native-subset-model"],fetchMode:"per_task",usageSchema:{count:{type:"number",unit:"count"}},protocols:[{name:"openai_responses",models:[%q],supports:["sync","stream","background"]}]};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};export const protocols={openai_responses:{decodeRequest:function(){return {}},renderEvents:function(){return {events:[],done:false}},renderFinal:function(){return {}}}};`, key, name, name)
			plugin, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
			require.NoError(t, err)
			require.NoError(t, db.Create(&model.Channel{Id: 911, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: name + ",native-subset-model", Key: "fixture-only"}).Error)
			for _, modelName := range []string{name, "native-subset-model"} {
				require.NoError(t, db.Create(&model.Ability{ChannelId: 911, Model: modelName, Enabled: true}).Error)
			}
			if tc.nativePrice {
				require.NoError(t, db.Create(&model.Option{Key: "ModelPrice", Value: `{"manifest-endpoint-model":"0","native-subset-model":"0"}`}).Error)
			}
			if tc.pluginPrice || tc.invalidPlugin {
				expression := `u("count") * 0.01`
				if tc.invalidPlugin {
					expression = `u("undeclared_fact") * 0.01`
				}
				encoded, err := common.Marshal(map[string]string{billing_setting.PluginBillingExprKey(key, name): expression, billing_setting.PluginBillingExprKey(key, "native-subset-model"): expression})
				require.NoError(t, err)
				require.NoError(t, db.Create(&model.Option{Key: billing_setting.PluginBillingExprOption, Value: string(encoded)}).Error)
			}
			manifest, err := BuildModelManifest(context.Background(), ModelManifestRequest{UserID: userID, TokenID: tokenID})
			require.NoError(t, err)
			endpoints := map[string][]constant.EndpointType{}
			for _, entry := range manifest.Models {
				endpoints[entry.ID] = entry.SupportedEndpointTypes
			}
			assert.Equal(t, tc.wantChat, slices.Contains(endpoints[name], constant.EndpointTypeOpenAI))
			assert.Equal(t, tc.wantResponses, slices.Contains(endpoints[name], constant.EndpointTypeOpenAIResponse))
			assert.Equal(t, tc.nativePrice, slices.Contains(endpoints["native-subset-model"], constant.EndpointTypeOpenAIResponse), "a model outside a protocol's declared subset must use native pricing")
			_, nativeErr := model.ResolveModelBillingPrice(context.Background(), name, "")
			assert.Equal(t, tc.wantChat, nativeErr == nil)
			_, pluginErr := model.ResolveTaskModelBillingPrice(context.Background(), name, name, model.TaskPluginRef{Key: key, Version: plugin.Meta.Version, SourceHash: plugin.SourceHash})
			assert.Equal(t, tc.wantResponses, pluginErr == nil)
			for format, available := range map[string]bool{"pi": tc.wantChat, "codex": tc.wantResponses} {
				_, exportErr := ExportModelClientConfig(manifest, format, name, "https://gateway.example.test")
				assert.Equal(t, available, exportErr == nil, format)
			}
			require.NoError(t, db.Create(&model.UserChannelEntitlement{UserID: userID, ChannelID: 911, Source: model.RouteSourcePlatform, Status: model.RouteEntitlementStatusRevoked, RevokedAt: 1}).Error)
			manifest, err = BuildModelManifest(context.Background(), ModelManifestRequest{UserID: userID, TokenID: tokenID})
			require.NoError(t, err)
			assert.Empty(t, manifest.Models, "a price never overrides revoked channel access")
		})
	}
}

func TestModelManifestUnpricedPreferenceCannotBypassExecutor(t *testing.T) {
	for _, preference := range []string{"self-use", "user-setting"} {
		for _, scenario := range []string{"unconfigured", "ineligible-channel", "invalid-expression"} {
			t.Run(preference+"/"+scenario, func(t *testing.T) {
				db, userID, tokenID := setupModelManifestTest(t)
				previous := jsplugin.DefaultRegistry
				jsplugin.DefaultRegistry = jsplugin.NewRegistry()
				t.Cleanup(func() { jsplugin.DefaultRegistry = previous })
				if preference == "self-use" {
					operation_setting.SelfUseModeEnabled = true
				} else {
					require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("setting", `{"accept_unset_model_ratio_model":true}`).Error)
				}
				pluginChannelType := constant.ChannelTypeOpenAI
				if scenario == "ineligible-channel" {
					pluginChannelType = constant.ChannelTypeAnthropic
				}
				const name, key = "manifest-unpriced-model", "manifest-unpriced-provider"
				source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:"Fixture",version:"1.0.0",author:{name:"Test"},channelTypes:[%d],models:[%q],fetchMode:"per_task",usageSchema:{count:{type:"number",unit:"count"}},protocols:[{name:"openai_responses",supports:["sync","stream","background"]}]};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};export const protocols={openai_responses:{decodeRequest:function(){return {}},renderEvents:function(){return {events:[],done:false}},renderFinal:function(){return {}}}};`, key, pluginChannelType, name)
				plugin, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
				require.NoError(t, err)
				channel := &model.Channel{Id: 951, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: name, Key: "fixture-only"}
				require.NoError(t, db.Create(channel).Error)
				require.NoError(t, db.Create(&model.Ability{ChannelId: 951, Model: name, Enabled: true}).Error)
				if scenario == "invalid-expression" {
					encoded, err := common.Marshal(map[string]string{billing_setting.PluginBillingExprKey(key, name): `u("undefined") * 0.01`})
					require.NoError(t, err)
					require.NoError(t, db.Create(&model.Option{Key: billing_setting.PluginBillingExprOption, Value: string(encoded)}).Error)
					_, err = model.ResolveTaskModelBillingPrice(context.Background(), name, name, model.TaskPluginRef{Key: key, Version: plugin.Meta.Version, SourceHash: plugin.SourceHash})
					require.ErrorContains(t, err, "not configured for the executing task plugin")
				}
				if scenario == "ineligible-channel" {
					require.False(t, TaskPluginCandidateChannelEligible(channel, key, []string{key}))
				}
				manifest, err := BuildModelManifest(context.Background(), ModelManifestRequest{UserID: userID, TokenID: tokenID})
				require.NoError(t, err)
				require.Len(t, manifest.Models, 1, "the unpriced native Chat endpoint remains visible by explicit preference")
				assert.Contains(t, manifest.Models[0].SupportedEndpointTypes, constant.EndpointTypeOpenAI)
				assert.False(t, manifest.Models[0].PriceConfigured)
				assert.Equal(t, scenario == "unconfigured", slices.Contains(manifest.Models[0].SupportedEndpointTypes, constant.EndpointTypeOpenAIResponse))
				_, err = ExportModelClientConfig(manifest, "codex", name, "https://gateway.example.test")
				assert.Equal(t, scenario == "unconfigured", err == nil)
			})
		}
	}
}

func TestModelManifestPricesTheSelectedSharedEndpointPlugin(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	previous := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previous })
	const name = "manifest-shared-executor"
	for _, key := range []string{"manifest-first", "manifest-second"} {
		source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:"Fixture",version:"1.0.0",author:{name:"Test"},upstreams:["new_api"],models:[%q],fetchMode:"per_task",usageSchema:{count:{type:"number",unit:"count"}},protocols:[{name:"openai_responses",supports:["sync","stream","background"]}]};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};export const protocols={openai_responses:{decodeRequest:function(){return {}},renderEvents:function(){return {events:[],done:false}},renderFinal:function(){return {}}}};`, key, name)
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	candidates := jsplugin.DefaultRegistry.Generation().LookupEndpointCandidates("POST", "/v1/responses", name)
	require.Len(t, candidates, 2)
	setting := `{"task_extend_plugin_keys":["manifest-first","manifest-second"]}`
	channel := &model.Channel{Id: 952, Type: constant.ChannelTypeNewAPI, Status: common.ChannelStatusEnabled, Models: name, Key: "fixture-only", Setting: &setting}
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: 952, Model: name, Enabled: true}).Error)
	first := candidates[0].Plugin
	for _, binding := range candidates {
		require.True(t, TaskPluginChannelEligible(channel, binding.Plugin.Meta.Key))
	}
	prices := map[string]string{billing_setting.PluginBillingExprKey(candidates[1].Plugin.Meta.Key, name): `u("count") * 0.01`}
	encoded, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: billing_setting.PluginBillingExprOption, Value: string(encoded)}).Error)
	_, err = model.ResolveTaskModelBillingPrice(context.Background(), name, name, model.TaskPluginRef{Key: first.Meta.Key, Version: first.Meta.Version, SourceHash: first.SourceHash})
	require.Error(t, err, "execution selects the first eligible plugin before pricing")
	request := ModelManifestRequest{UserID: userID, TokenID: tokenID}
	manifest, err := BuildModelManifest(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	assert.Contains(t, manifest.Models[0].SupportedEndpointTypes, constant.EndpointTypeTaskPlugin, "explicit native routes can still address the separately priced plugin")
	assert.NotContains(t, manifest.Models[0].SupportedEndpointTypes, constant.EndpointTypeOpenAIResponse)
	_, err = ExportModelClientConfig(manifest, "codex", name, "https://gateway.example.test")
	require.Error(t, err)

	prices[billing_setting.PluginBillingExprKey(first.Meta.Key, name)] = `u("count") * 0.02`
	encoded, err = common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Option{}).Where(map[string]any{"key": billing_setting.PluginBillingExprOption}).Update("value", string(encoded)).Error)
	manifest, err = BuildModelManifest(context.Background(), request)
	require.NoError(t, err)
	require.Len(t, manifest.Models, 1)
	assert.Contains(t, manifest.Models[0].SupportedEndpointTypes, constant.EndpointTypeOpenAIResponse)
	_, err = model.ResolveTaskModelBillingPrice(context.Background(), name, name, model.TaskPluginRef{Key: first.Meta.Key, Version: first.Meta.Version, SourceHash: first.SourceHash})
	require.NoError(t, err)
	_, err = ExportModelClientConfig(manifest, "codex", name, "https://gateway.example.test")
	require.NoError(t, err)
}
