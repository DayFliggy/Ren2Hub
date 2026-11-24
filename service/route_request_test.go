package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTaskPluginEndpointLookupNormalizesMappingAlias(t *testing.T) {
	db := setupRouteTest(t)
	key := "route-alias"
	_, err := jsplugin.DefaultRegistry.Register(channelSelectEndpointPluginSource(key, 0), jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(key) })
	createRouteChannel(t, db, 11, "Public-Alias")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Updates(map[string]any{"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"route-alias"}`, "model_mapping": `{"Public-Alias":"task-model"}`}).Error)
	model.InitChannelCache()

	generation := jsplugin.DefaultRegistry.Generation()
	plugin, ok := generation.Get(key)
	require.True(t, ok)
	require.Equal(t, "Public-Alias", NormalizeTaskPluginSubmitModel(generation, plugin, "PUBLIC-ALIAS"))

	endpoint, ok := LookupTaskPluginEndpoint(generation, http.MethodPost, "/v1/responses", "PUBLIC-ALIAS")
	require.True(t, ok)
	require.Equal(t, "Public-Alias", endpoint.Model)
	filter := TaskPluginEndpointFilter(endpoint.Candidates, "/v1/responses")
	var row model.Channel
	require.NoError(t, db.First(&row, 11).Error)
	ok, _ = model.ChannelSatisfiesFilters(&row, endpoint.Model, []dto.ChannelFilter{filter})
	require.True(t, ok)
	// A resolved filter retains its generation snapshot even after unregistering.
	require.NoError(t, jsplugin.DefaultRegistry.Unregister(key))
	ok, _ = model.ChannelSatisfiesFilters(&row, endpoint.Model, []dto.ChannelFilter{filter})
	require.True(t, ok)
}

func TestTaskSubmitOrderingUsesSubmitObservations(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	key := "submit-order"
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
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Updates(map[string]any{"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"submit-order"}`}).Error)
	}
	// Unknown observations deliberately rank the reverse way from VideoSubmit.
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

	generation := jsplugin.DefaultRegistry.Generation()
	plugin, ok := generation.Get(key)
	require.True(t, ok)
	for _, path := range []string{"/v1/tasks/" + key, "/v1/videos"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, path, nil)
		common.SetContextKey(c, constant.ContextKeyTokenId, 7)
		c.Set("relay_mode", relayconstant.RelayModeVideoSubmit)
		c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: generation, Plugin: plugin})
		AppendTaskPluginIdentityFilter(c, key)
		selected, selErr := SelectChannelForRequest(c, "task-model", nil)
		require.Nil(t, selErr, path)
		require.Equal(t, 22, selected.Id, path)
		ReleaseRouteChannel(c)
	}
}

// #8335: several plugins may declare one built-in channel type. Dispatch
// derives the shared identity filter: the first candidate in key order
// executes, and a channel of the shared type serves it.
func TestSharedChannelTypePluginsDispatchIdentity(t *testing.T) {
	db := setupRouteTest(t)
	for _, key := range []string{"shared-beta", "shared-alpha"} {
		_, err := jsplugin.DefaultRegistry.Register(channelSelectEndpointPluginSource(key, constant.ChannelTypeGemini), jsplugin.Options{})
		require.NoError(t, err, "a second plugin may declare the same channel type")
		t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	}
	createRouteChannel(t, db, 11, "task-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("type", constant.ChannelTypeGemini).Error)

	generation := jsplugin.DefaultRegistry.Generation()
	endpoint, ok := LookupTaskPluginEndpoint(generation, http.MethodPost, "/v1/responses", "task-model")
	require.True(t, ok)
	c := routeTestContext(t)
	c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: generation, Plugin: endpoint.Plugin})
	c.Set(jsplugin.ContextKeyPinnedEndpoint, endpoint)
	AppendTaskPluginIdentityFilter(c, endpoint.Plugin.Meta.Key)
	filters := GetChannelConstraints(c).Filters
	dispatched := filters[len(filters)-1]
	require.Equal(t, "shared-alpha", dispatched.TaskPluginKey)
	require.Equal(t, []int{constant.ChannelTypeGemini}, dispatched.TaskPluginChannelTypes)
	require.Equal(t, []string{"shared-alpha", "shared-beta"}, dispatched.TaskPluginKeys)
	selected, selErr := SelectChannelForRequest(c, "task-model", nil)
	require.Nil(t, selErr)
	require.Equal(t, 11, selected.Id)
}
