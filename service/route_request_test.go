package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRoutePreviewUsesTransportAndPluginEligibility(t *testing.T) {
	db := setupRouteTest(t)
	key := "preview-standard"
	source := channelSelectEndpointPluginSource(key, 0)
	// A preview must never execute a decoder, even on a claimed endpoint.
	source = strings.Replace(source, `return {kind: "submit", model: "task-model", requestBody: ctx.body.value};`, `throw new Error("preview must not execute JS");`, 1)
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	for _, id := range []int{11, 22, 33, 44} {
		createRouteChannel(t, db, id, "task-model")
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("setting", `{"responses_websocket_enabled":true}`).Error)
	for id, plugin := range map[int]string{33: key, 44: "unrelated"} {
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Updates(map[string]any{"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"` + plugin + `"}`}).Error)
	}
	preview := func(q TokenRoutePreviewQuery) TokenRoutePreview {
		t.Helper()
		result, err := PreviewTokenRouting(context.Background(), 17, 7, q)
		require.NoError(t, err)
		for _, id := range []int{11, 22, 33, 44} {
			require.Zero(t, ChannelInFlight(id))
		}
		return result
	}
	eligible := func(p TokenRoutePreview) []int {
		var ids []int
		for _, entry := range p.Entries {
			if entry.Eligible {
				ids = append(ids, entry.ChannelID)
			}
		}
		return ids
	}
	q := TokenRoutePreviewQuery{Model: "task-model", Path: "/v1/responses"}
	require.ElementsMatch(t, []int{11, 22, 33}, eligible(preview(q)), "host endpoint permits ordinary channels and its owning plugin only")
	q.Transport = "websocket"
	ws := preview(q)
	require.Equal(t, "websocket", ws.Transport)
	require.True(t, ws.Stream)
	require.Equal(t, []int{22}, eligible(ws))
	q.Transport, q.Path = "http", "/v1/tasks/"+key
	q.Model = "TASK-MODEL"
	generic := preview(q)
	require.Equal(t, "task-model", generic.Model)
	require.Equal(t, []int{33}, eligible(generic))
	q.Path, q.Model = "/v1/chat/completions", "task-model"
	require.ElementsMatch(t, []int{11, 22}, eligible(preview(q)), "ordinary paths cannot select unrelated task plugins")
	saveRouteTest(t, 44, 33, 22)
	q.Path, q.Transport = "/v1/responses", "websocket"
	manual := preview(q)
	require.Equal(t, []int{44, 33, 22}, []int{manual.Entries[0].ChannelID, manual.Entries[1].ChannelID, manual.Entries[2].ChannelID})
	require.Equal(t, []int{22}, eligible(manual))
	_, err = PreviewTokenRouting(context.Background(), 999, 7, q)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	for _, q := range []TokenRoutePreviewQuery{
		{Model: "task-model", Path: "/vendor/custom"},
		{Model: "task-model", Path: "/v1/chat/completions", Transport: "websocket"},
		{Model: "task-model", Path: "/v1/tasks/missing"},
	} {
		_, err := PreviewTokenRouting(context.Background(), 17, 7, q)
		require.ErrorIs(t, err, ErrPreviewPathUnsupported)
	}
}

func TestRoutePreviewRegistrySnapshotAndCanonicalFallback(t *testing.T) {
	db := setupRouteTest(t)
	key := "preview-refresh"
	_, err := jsplugin.DefaultRegistry.Register(channelSelectEndpointPluginSource(key, 0), jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(key) })
	createRouteChannel(t, db, 11, "task-model")
	createRouteChannel(t, db, 22, "task-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = 11").Updates(map[string]any{"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"preview-refresh"}`}).Error)
	q := TokenRoutePreviewQuery{Model: "TASK-MODEL", Path: "/v1/responses"}
	resolved, filters, fallback, err := resolvePreviewRequest(q)
	require.NoError(t, err)
	require.Equal(t, "task-model", fallback.model)
	first, err := PreviewTokenRouting(context.Background(), 17, 7, q)
	require.NoError(t, err)
	require.Equal(t, "task-model", first.Model)
	entries := func(p TokenRoutePreview) map[int]TokenRoutePreviewEntry {
		out := map[int]TokenRoutePreviewEntry{}
		for _, entry := range p.Entries {
			out[entry.ChannelID] = entry
		}
		return out
	}
	require.True(t, entries(first)[11].Eligible)
	require.False(t, entries(first)[22].Eligible, "canonical fallback must apply the pinned plugin identity")
	require.Equal(t, "task_plugin_identity", entries(first)[22].Reason)
	exact, err := PreviewTokenRouting(context.Background(), 17, 7, TokenRoutePreviewQuery{Model: "task-model", Path: q.Path})
	require.NoError(t, err)
	require.True(t, entries(exact)[11].Eligible)
	require.True(t, entries(exact)[22].Eligible, "original spelling may still select an ordinary relay channel")
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(key))
	var row model.Channel
	require.NoError(t, db.First(&row, 11).Error)
	ok, _ := model.ChannelSatisfiesFilters(&row, resolved.Model, filters)
	require.True(t, ok, "a resolved request retains its generation after unregister")
	current, err := PreviewTokenRouting(context.Background(), 17, 7, TokenRoutePreviewQuery{Model: "task-model", Path: "/v1/responses"})
	require.NoError(t, err)
	require.False(t, entries(current)[11].Eligible, "the next request sees the registry update")
}

func TestRoutePreviewNormalizesMappingAliasAndPathModel(t *testing.T) {
	db := setupRouteTest(t)
	key := "preview-alias"
	_, err := jsplugin.DefaultRegistry.Register(channelSelectEndpointPluginSource(key, 0), jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(key) })
	createRouteChannel(t, db, 11, "Public-Alias")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = 11").Updates(map[string]any{"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"preview-alias"}`, "model_mapping": `{"Public-Alias":"task-model"}`}).Error)
	model.InitChannelCache()
	for _, path := range []string{"/v1/tasks/" + key, "/v1/responses"} {
		p, err := PreviewTokenRouting(context.Background(), 17, 7, TokenRoutePreviewQuery{Model: "PUBLIC-ALIAS", Path: path})
		require.NoError(t, err)
		require.Equal(t, "Public-Alias", p.Model)
		require.Len(t, p.Entries, 1)
		require.True(t, p.Entries[0].Eligible)
	}
	q, _, _, err := resolvePreviewRequest(TokenRoutePreviewQuery{Model: "ignored-body", Path: "/v1beta/models/gemini-test:streamGenerateContent"})
	require.NoError(t, err)
	require.Equal(t, "gemini-test", q.Model)
	require.True(t, q.Stream)
	q, _, _, err = resolvePreviewRequest(TokenRoutePreviewQuery{Model: "ignored-body", Path: "/v1/tools/search"})
	require.NoError(t, err)
	require.Equal(t, constant.DeepSeekSearchModel, q.Model)
}

func TestRoutePreviewTaskOrderingUsesSubmitObservations(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	key := "preview-order"
	source := strings.ReplaceAll(channelSelectEndpointPluginSource(key, 0), "openai_responses", "openai_video")
	source = strings.ReplaceAll(source, `, supports: ["stream", "sync", "background"]`, "")
	source = source[:strings.Index(source, "export const protocols")] + `
export const protocols = {openai_video: {
  decodeRequest: function(ctx) { return ctx; }, render: function(ctx, task) { return task; }
}};
export function listArtifacts() { return []; }
export function buildContentRequest() { return {}; }
`
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	for _, id := range []int{11, 22} {
		createRouteChannel(t, db, id, "task-model")
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Updates(map[string]any{"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"preview-order"}`}).Error)
	}
	// Unknown observations deliberately rank the reverse way from submits.
	require.NoError(t, db.Model(&model.Channel{}).Where("id = 11").Update("weight", 1000000).Error)
	now := time.Now()
	for _, id := range []int{11, 22} {
		routeQualities.invalidate(id)
		t.Cleanup(func() { routeQualities.invalidate(id) })
	}
	for range routeMinimumSamples {
		routeQualities.add(routeQualityKey{"task-model", routeObservationKey{11, "task-model", relayconstant.RelayModeVideoSubmit, false}}, true, 12000, 0, 0, now)
		routeQualities.add(routeQualityKey{"task-model", routeObservationKey{22, "task-model", relayconstant.RelayModeVideoSubmit, false}}, true, 100, 0, 0, now)
		routeQualities.add(routeQualityKey{"task-model", routeObservationKey{22, "task-model", relayconstant.RelayModeUnknown, false}}, true, 12000, 0, 0, now)
	}
	routeQualities.evaluate(now)
	for _, path := range []string{"/v1/tasks/" + key, "/v1/videos"} {
		p, err := PreviewTokenRouting(context.Background(), 17, 7, TokenRoutePreviewQuery{Model: "task-model", Path: path})
		require.NoError(t, err)
		require.Len(t, p.Entries, 2)
		require.True(t, p.Entries[0].Eligible)
		require.Equal(t, 22, p.Entries[0].ChannelID, path)
	}
}

func TestRoutePreviewProviderUsesDeclaredPathAndModel(t *testing.T) {
	db := setupRouteTest(t)
	key := "preview-provider"
	_, err := jsplugin.DefaultRegistry.Register(`export const meta = {
	  apiVersion: 1, key: "preview-provider", name: "Preview provider", version: "1.0.0", author: {name: "Test"},
	  relay: [{incoming_path: "/v1/chat/completions", upstream_path: "/chat", models: ["provider-model"]}]
	};`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	createRouteChannel(t, db, 11, "provider-model,other-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Updates(map[string]any{
		"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"preview-provider"}`,
	}).Error)
	for _, testCase := range []struct {
		path, model string
		eligible    bool
	}{
		{"/v1/chat/completions", "provider-model", true},
		{"/v1/embeddings", "provider-model", false},
		{"/v1/chat/completions", "other-model", false},
	} {
		preview, err := PreviewTokenRouting(context.Background(), 17, 7, TokenRoutePreviewQuery{Path: testCase.path, Model: testCase.model})
		require.NoError(t, err)
		require.Len(t, preview.Entries, 1)
		require.Equal(t, testCase.eligible, preview.Entries[0].Eligible, "%s %s", testCase.path, testCase.model)
		require.Zero(t, ChannelInFlight(11), "preview does not acquire capacity")
	}
}

// #8335: several plugins may declare one built-in channel type. Preview and
// dispatch derive the same identity filter for a shared endpoint: the first
// candidate in key order executes, and a channel of the shared type serves it.
func TestSharedChannelTypePluginsPreviewAndDispatchAgree(t *testing.T) {
	db := setupRouteTest(t)
	for _, key := range []string{"shared-beta", "shared-alpha"} {
		_, err := jsplugin.DefaultRegistry.Register(channelSelectEndpointPluginSource(key, constant.ChannelTypeGemini), jsplugin.Options{})
		require.NoError(t, err, "a second plugin may declare the same channel type")
		t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	}
	createRouteChannel(t, db, 11, "task-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("type", constant.ChannelTypeGemini).Error)

	q := TokenRoutePreviewQuery{Model: "task-model", Path: "/v1/responses"}
	_, _, fallback, err := resolvePreviewRequest(q)
	require.NoError(t, err)
	previewed := fallback.filters[len(fallback.filters)-1]
	require.Equal(t, "shared-alpha", previewed.TaskPluginKey)
	require.Equal(t, []int{constant.ChannelTypeGemini}, previewed.TaskPluginChannelTypes)
	require.Equal(t, []string{"shared-alpha", "shared-beta"}, previewed.TaskPluginKeys)
	preview, err := PreviewTokenRouting(context.Background(), 17, 7, q)
	require.NoError(t, err)
	require.Len(t, preview.Entries, 1)
	require.True(t, preview.Entries[0].Eligible)

	generation := jsplugin.DefaultRegistry.Generation()
	endpoint, ok := LookupTaskPluginEndpoint(generation, "POST", q.Path, q.Model)
	require.True(t, ok)
	c := routeTestContext(t)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: generation, Plugin: endpoint.Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, endpoint)
	AppendTaskPluginIdentityFilter(c, endpoint.Plugin.Meta.Key)
	filters := GetChannelConstraints(c).Filters
	dispatched := filters[len(filters)-1]
	require.Equal(t, previewed.TaskPluginKey, dispatched.TaskPluginKey)
	require.Equal(t, previewed.TaskPluginChannelTypes, dispatched.TaskPluginChannelTypes)
	require.Equal(t, previewed.TaskPluginKeys, dispatched.TaskPluginKeys)
	selected, selErr := SelectChannelForRequest(c, q.Model, nil)
	require.Nil(t, selErr)
	require.Equal(t, 11, selected.Id)
}
