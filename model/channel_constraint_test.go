package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestChannelSatisfiesFiltersPerChannel keeps the per-channel cases of the
// removed id-list filter: identity keys, legacy task types, request paths and
// first-violation attribution in filterEvalOrder regardless of slice order.
func TestChannelSatisfiesFiltersPerChannel(t *testing.T) {
	alphaSetting := `{"task_plugin_key":"alpha"}`
	betaSetting := `{"task_plugin_key":"beta"}`
	alpha := &Channel{Id: 900001, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Setting: &alphaSetting}
	beta := &Channel{Id: 900002, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Setting: &betaSetting}
	ordinary := &Channel{Id: 900003, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	kling := &Channel{Id: 900004, Type: constant.ChannelTypeKling, Status: common.ChannelStatusEnabled}
	jimeng := &Channel{Id: 900005, Type: constant.ChannelTypeJimeng, Status: common.ChannelStatusEnabled}
	matchingCustom := &Channel{Id: 900010, Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	matchingCustom.SetOtherSettings(kitdto.ChannelOtherSettings{
		AdvancedCustom: &kitdto.AdvancedCustomConfig{
			Routes: []kitdto.AdvancedCustomRoute{{
				IncomingPath: "/v1/chat/completions",
				Models:       []string{"gpt-4"},
			}},
		},
	})
	otherCustom := &Channel{Id: 900011, Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	otherCustom.SetOtherSettings(kitdto.ChannelOtherSettings{
		AdvancedCustom: &kitdto.AdvancedCustomConfig{
			Routes: []kitdto.AdvancedCustomRoute{{
				IncomingPath: "/v1/responses",
				Models:       []string{"gpt-4"},
			}},
		},
	})

	pathFilter := dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: "/v1/chat/completions"}
	emptyPathFilter := dto.ChannelFilter{Kind: dto.FilterRequestPath, RequestPath: ""}

	tests := []struct {
		name     string
		channel  *Channel
		model    string
		filters  []dto.ChannelFilter
		wantOK   bool
		wantKind dto.ChannelFilterKind
	}{
		{"identity keeps matching type-59 key", alpha, "shared", identityFilters("alpha", nil), true, ""},
		{"identity drops other type-59 key", beta, "shared", identityFilters("alpha", nil), false, dto.FilterTaskPluginIdentity},
		{"identity empty key drops type-59", alpha, "shared", identityFilters("", nil), false, dto.FilterTaskPluginIdentity},
		{"identity empty key keeps ordinary channel", ordinary, "ordinary", identityFilters("", nil), true, ""},
		{"identity keeps listed legacy type", kling, "legacy", identityFilters("legacy-alpha", []int{constant.ChannelTypeKling}), true, ""},
		{"identity drops unlisted legacy type", jimeng, "legacy", identityFilters("legacy-alpha", []int{constant.ChannelTypeKling}), false, dto.FilterTaskPluginIdentity},
		{"identity keeps every listed legacy type", jimeng, "legacy", identityFilters("legacy-alpha", []int{constant.ChannelTypeKling, constant.ChannelTypeJimeng}), true, ""},
		{"identity keyed with no types drops legacy", kling, "legacy", identityFilters("legacy-alpha", nil), false, dto.FilterTaskPluginIdentity},
		{"empty request path passes type-58", otherCustom, "gpt-4", []dto.ChannelFilter{emptyPathFilter}, true, ""},
		{"request path keeps ordinary", ordinary, "gpt-4", []dto.ChannelFilter{pathFilter}, true, ""},
		{"request path keeps matching type-58", matchingCustom, "gpt-4", []dto.ChannelFilter{pathFilter}, true, ""},
		{"request path drops unmatched type-58", otherCustom, "gpt-4", []dto.ChannelFilter{pathFilter}, false, dto.FilterRequestPath},
		{"identity is blamed after path passes", matchingCustom, "gpt-4", []dto.ChannelFilter{pathFilter, identityFilters("missing", nil)[0]}, false, dto.FilterTaskPluginIdentity},
		{"path is blamed first whatever the slice order", otherCustom, "gpt-4", []dto.ChannelFilter{identityFilters("", nil)[0], pathFilter}, false, dto.FilterRequestPath},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			ok, kind := ChannelSatisfiesFilters(testCase.channel, testCase.model, testCase.filters)
			assert.Equal(t, testCase.wantOK, ok)
			assert.Equal(t, testCase.wantKind, kind)
		})
	}
}

func TestChannelSatisfiesFilters(t *testing.T) {
	alphaSetting := `{"task_plugin_key":"alpha"}`
	alpha := &Channel{Id: 1, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Setting: &alphaSetting}
	ordinary := &Channel{Id: 2, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	custom := &Channel{Id: 3, Type: constant.ChannelTypeAdvancedCustom, Status: common.ChannelStatusEnabled}
	custom.SetOtherSettings(kitdto.ChannelOtherSettings{
		AdvancedCustom: &kitdto.AdvancedCustomConfig{
			Routes: []kitdto.AdvancedCustomRoute{{
				IncomingPath: "/v1/chat/completions",
				Models:       []string{"gpt-4"},
			}},
		},
	})

	ok, kind := ChannelSatisfiesFilters(nil, "gpt-4", nil)
	assert.False(t, ok)
	assert.Equal(t, dto.ChannelFilterKind(""), kind)

	ok, kind = ChannelSatisfiesFilters(alpha, "shared", identityFilters("alpha", nil))
	require.True(t, ok)
	assert.Equal(t, dto.ChannelFilterKind(""), kind)

	ok, kind = ChannelSatisfiesFilters(alpha, "shared", identityFilters("beta", nil))
	assert.False(t, ok)
	assert.Equal(t, dto.FilterTaskPluginIdentity, kind)

	ok, kind = ChannelSatisfiesFilters(ordinary, "gpt-4", []dto.ChannelFilter{{
		Kind:        dto.FilterRequestPath,
		RequestPath: "/v1/chat/completions",
	}})
	require.True(t, ok)
	assert.Equal(t, dto.ChannelFilterKind(""), kind)

	ok, kind = ChannelSatisfiesFilters(custom, "gpt-4", []dto.ChannelFilter{{
		Kind:        dto.FilterRequestPath,
		RequestPath: "/v1/responses",
	}})
	assert.False(t, ok)
	assert.Equal(t, dto.FilterRequestPath, kind)
}

func TestNativeCountFiltersOnlyProviderAPIKeyChannels(t *testing.T) {
	for _, route := range []struct {
		path     string
		provider int
	}{
		{"/v1/messages/count_tokens", constant.ChannelTypeAnthropic},
		{"/v1/models/gemini-2.5-flash:countTokens", constant.ChannelTypeGemini},
		{"/v1beta/models/gemini-2.5-flash:countTokens", constant.ChannelTypeGemini},
	} {
		for _, provider := range []int{constant.ChannelTypeAnthropic, constant.ChannelTypeGemini, constant.ChannelTypeVertexAi, constant.ChannelTypeOpenAI, constant.ChannelTypeAdvancedCustom, constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI, constant.ChannelTypeTaskPlugin} {
			ch := &Channel{Id: provider, Type: provider, Status: common.ChannelStatusEnabled}
			ok, _ := ChannelSatisfiesFilters(ch, "shared-model", []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: route.path}})
			require.Equal(t, provider == route.provider, ok, "path=%s provider=%d", route.path, provider)
		}
	}
}

// A task plugin channel takes a Responses WebSocket session when the switch
// is on and its plugin's /v1/responses route for the model has no converter,
// the rule an Advanced Custom channel follows.
func TestTaskPluginChannelResponsesWebSocket(t *testing.T) {
	for key, relay := range map[string]string{
		"ws-native":    `[{incoming_path: "/v1/responses", upstream_path: "/responses", models: ["m1"]}]`,
		"ws-converted": `[{incoming_path: "/v1/responses", upstream_path: "/responses", converter: "plugin"}]`,
	} {
		source := `export const meta = {apiVersion: 1, key: "` + key + `", name: "WS", version: "1.0.0", author: {name: "Test"}, relay: ` + relay + `};`
		if key == "ws-converted" {
			source += `export function convertRequest(ctx, req) { return {body: req.body}; } export function convertResponse(ctx, res) { return {body: res.body}; } export function convertStreamEvent() { return {}; }`
		}
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
		t.Cleanup(func() { _ = jsplugin.DefaultRegistry.Unregister(key) })
	}
	channel := func(key string, enabled bool) *Channel {
		ch := &Channel{Id: 1, Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled}
		ch.SetSetting(kitdto.ChannelSettings{TaskPluginKey: key, ResponsesWebSocketEnabled: enabled})
		return ch
	}
	filters := []dto.ChannelFilter{{Kind: dto.FilterResponsesWebSocket}}

	ok, _ := ChannelSatisfiesFilters(channel("ws-native", true), "m1", filters)
	assert.True(t, ok)
	ok, kind := ChannelSatisfiesFilters(channel("ws-native", false), "m1", filters)
	assert.False(t, ok, "the channel switch is off")
	assert.Equal(t, dto.FilterResponsesWebSocket, kind)
	ok, _ = ChannelSatisfiesFilters(channel("ws-native", true), "m2", filters)
	assert.False(t, ok, "the route does not serve the model")
	ok, _ = ChannelSatisfiesFilters(channel("ws-converted", true), "m1", filters)
	assert.False(t, ok, "a converted route cannot forward native events")
	ok, _ = ChannelSatisfiesFilters(channel("ws-missing", true), "m1", filters)
	assert.False(t, ok)
}

func TestProviderChannelCompactPathUsesDeclaredModelRoute(t *testing.T) {
	const key = "provider-compact"
	_, err := jsplugin.DefaultRegistry.Register(`export const meta = {apiVersion: 1, key: "provider-compact", name: "Compact", version: "1.0.0", author: {name: "Test"}, relay: [
		{incoming_path: "/v1/responses/compact", upstream_path: "/compact", models: ["supported"]}
	]};`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	channel := &Channel{Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled}
	channel.SetSetting(kitdto.ChannelSettings{TaskPluginKey: key})
	filters := []dto.ChannelFilter{
		{Kind: dto.FilterRequestPath, RequestPath: "/v1/responses/compact"},
		{Kind: dto.FilterTaskPluginIdentity, RequestPath: "/v1/responses/compact"},
	}
	ok, _ := ChannelSatisfiesFilters(channel, "supported", filters)
	require.True(t, ok)
	ok, _ = ChannelSatisfiesFilters(channel, "other", filters)
	require.False(t, ok)
	channel.SetSetting(kitdto.ChannelSettings{TaskPluginKey: "uninstalled"})
	ok, _ = ChannelSatisfiesFilters(channel, "supported", filters)
	require.False(t, ok)
}

func TestProviderCompactUsesNativeFilterOrder(t *testing.T) {
	channel := &Channel{Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled}
	channel.SetSetting(kitdto.ChannelSettings{TaskPluginKey: "round3-uninstalled"})
	filters := []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: "/v1/responses/compact"}, {Kind: dto.FilterTaskPluginIdentity, RequestPath: "/v1/responses/compact"}}
	ok, kind := ChannelSatisfiesFilters(channel, "m", filters)
	require.False(t, ok)
	require.Equal(t, dto.FilterTaskPluginIdentity, kind)
}
