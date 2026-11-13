package service

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

var ErrPreviewPathUnsupported = errors.New("preview_path_unsupported")

// LookupTaskPluginEndpoint uses one immutable generation for both dispatch and
// static previews. It does not decode the body or execute plugin code.
func LookupTaskPluginEndpoint(g *jsplugin.RoutingGeneration, method, path, modelName string) (jsplugin.PinnedEndpoint, bool) {
	lookupModel, pinModel, mappedModel := modelName, modelName, ""
	if declared, ok := g.CanonicalModel(modelName); ok {
		lookupModel, pinModel = declared, declared
	} else if target, ok := model.ResolveTaskModelAlias(g, modelName); ok {
		if target.Declared == "" {
			return jsplugin.PinnedEndpoint{}, false
		}
		lookupModel, pinModel, mappedModel = target.Declared, target.Alias, target.Declared
	}
	candidates := g.LookupEndpointCandidates(method, path, lookupModel)
	if len(candidates) == 0 {
		return jsplugin.PinnedEndpoint{}, false
	}
	binding := candidates[0]
	return jsplugin.PinnedEndpoint{Generation: g, Plugin: binding.Plugin, Protocol: binding.Protocol,
		Operation: binding.Operation, Model: pinModel, MappedModel: mappedModel, Candidates: candidates}, true
}

func TaskPluginEndpointFilter(candidates []jsplugin.ProtocolBinding, requestPath string) dto.ChannelFilter {
	filter := dto.ChannelFilter{Kind: dto.FilterTaskPluginEndpoint, RequestPath: requestPath}
	for _, candidate := range candidates {
		filter.TaskPluginKeys = append(filter.TaskPluginKeys, candidate.Plugin.Meta.Key)
		filter.TaskPluginChannelTypes = append(filter.TaskPluginChannelTypes, candidate.Plugin.Meta.ChannelTypes...)
	}
	return filter
}

func NormalizeTaskPluginSubmitModel(g *jsplugin.RoutingGeneration, plugin *jsplugin.LoadedPlugin, name string) string {
	if slices.Contains(plugin.Meta.Models, name) {
		return name
	}
	if target, ok := model.ResolveTaskModelAlias(g, name); ok && target.Alias == name && target.PluginKey == plugin.Meta.Key {
		return name
	}
	if declared, ok := g.CanonicalModel(name); ok && slices.Contains(plugin.Meta.Models, declared) {
		return declared
	}
	if target, ok := model.ResolveTaskModelAlias(g, name); ok && target.PluginKey == plugin.Meta.Key && target.Alias != "" {
		return target.Alias
	}
	return name
}

func taskPluginIdentities(g *jsplugin.RoutingGeneration, candidates []*jsplugin.LoadedPlugin, expected string, shared bool) ([]int, []string) {
	if expected == "" || g == nil {
		return nil, nil
	}
	var types []int
	var keys []string
	found := false
	for _, plugin := range candidates {
		if plugin == nil {
			continue
		}
		found = found || plugin.Meta.Key == expected
		keys = append(keys, plugin.Meta.Key)
		for _, typ := range plugin.Meta.ChannelTypes {
			if typ == 0 || typ == constant.ChannelTypeTaskPlugin || slices.Contains(types, typ) {
				continue
			}
			if owner, ok := g.GetByChannelType(typ); shared && (!ok || owner != plugin) {
				continue
			}
			types = append(types, typ)
		}
	}
	if !found {
		return nil, nil
	}
	return types, keys
}

// taskPluginEndpointIdentities is shared by dispatch and canonical preview fallback.
func taskPluginEndpointIdentities(endpoint jsplugin.PinnedEndpoint, expected string) ([]int, []string) {
	if len(endpoint.Candidates) > 1 {
		plugins := make([]*jsplugin.LoadedPlugin, 0, len(endpoint.Candidates))
		for _, candidate := range endpoint.Candidates {
			plugins = append(plugins, candidate.Plugin)
		}
		if types, keys := taskPluginIdentities(endpoint.Generation, plugins, expected, true); len(keys) > 0 {
			return types, keys
		}
	}
	return taskPluginIdentities(endpoint.Generation, []*jsplugin.LoadedPlugin{endpoint.Plugin}, expected, false)
}

type previewFallback struct {
	model   string
	filters []dto.ChannelFilter
}

func resolvePreviewRequest(query TokenRoutePreviewQuery) (TokenRoutePreviewQuery, []dto.ChannelFilter, previewFallback, error) {
	if query.Transport == "" {
		query.Transport = "http"
	}
	filters := []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: query.Path}}
	if query.Transport == "websocket" {
		if query.Path != "/v1/responses" {
			return query, nil, previewFallback{}, ErrPreviewPathUnsupported
		}
		query.Stream = true
		return query, append(filters, dto.ChannelFilter{Kind: dto.FilterResponsesWebSocket}), previewFallback{}, nil
	}
	if query.Transport != "http" {
		return query, nil, previewFallback{}, model.ErrTokenRouteInvalid
	}
	if strings.HasPrefix(query.Path, "/v1/models/") || strings.HasPrefix(query.Path, "/v1beta/models/") {
		name, action, err := relayconstant.ParseGeminiModelAction(query.Path)
		if err != nil {
			return query, nil, previewFallback{}, ErrPreviewPathUnsupported
		}
		query.Model = name
		query.Stream = action == "streamGenerateContent" || query.Stream
	}
	switch query.Path {
	case "/v1/tools/search":
		query.Model = constant.DeepSeekSearchModel
	case "/v1/tools/fetch":
		query.Model = constant.DeepSeekFetchModel
	}
	generation := jsplugin.DefaultRegistry.Generation()
	if key, task := strings.CutPrefix(query.Path, "/v1/tasks/"); task {
		plugin, ok := generation.Get(key)
		if key == "" || strings.Contains(key, "/") || !ok {
			return query, nil, previewFallback{}, ErrPreviewPathUnsupported
		}
		query.Model = NormalizeTaskPluginSubmitModel(generation, plugin, query.Model)
		types, keys := taskPluginIdentities(generation, []*jsplugin.LoadedPlugin{plugin}, key, false)
		return query, append(filters, dto.ChannelFilter{Kind: dto.FilterTaskPluginIdentity, RequestPath: query.Path, TaskPluginKey: key,
			TaskPluginChannelTypes: types, TaskPluginKeys: keys}), previewFallback{}, nil
	}
	if _, _, host := jsplugin.LookupHostProtocolOperation(http.MethodPost, query.Path); host {
		if endpoint, ok := LookupTaskPluginEndpoint(generation, http.MethodPost, query.Path, query.Model); ok {
			// Dispatch tries the original spelling before canonical fallback; an
			// ordinary relay channel may serve that spelling exactly.
			filters = append(filters, TaskPluginEndpointFilter(endpoint.Candidates, query.Path))
			types, keys := taskPluginEndpointIdentities(endpoint, endpoint.Plugin.Meta.Key)
			fallback := previewFallback{model: endpoint.Model, filters: append(slices.Clone(filters), dto.ChannelFilter{
				Kind: dto.FilterTaskPluginIdentity, RequestPath: query.Path, TaskPluginKey: endpoint.Plugin.Meta.Key,
				TaskPluginChannelTypes: types, TaskPluginKeys: keys,
			})}
			return query, filters, fallback, nil
		}
	} else if !ordinaryPreviewPath(query.Path) {
		return query, nil, previewFallback{}, ErrPreviewPathUnsupported
	}
	return query, append(filters, dto.ChannelFilter{Kind: dto.FilterTaskPluginIdentity, RequestPath: query.Path}), previewFallback{}, nil
}

// These are body-independent POST entrypoints. Custom plugin routes require
// decoding, so cannot claim eligibility from only model/path/transport.
func ordinaryPreviewPath(path string) bool {
	switch path {
	case "/v1/chat/completions", "/v1/completions", "/v1/messages", "/v1/messages/count_tokens",
		"/v1/responses/compact", "/v1/alpha/search", "/v1/tools/search", "/v1/tools/fetch", "/v1/edits",
		"/v1/embeddings", "/v1/moderations", "/v1/rerank", "/v1/audio/speech", "/v1/audio/transcriptions", "/v1/audio/translations":
		return true
	}
	if strings.HasPrefix(path, "/v1/engines/") {
		parts := strings.Split(strings.TrimPrefix(path, "/v1/engines/"), "/")
		return len(parts) == 2 && parts[0] != "" && parts[1] == "embeddings"
	}
	if strings.HasPrefix(path, "/v1/models/") || strings.HasPrefix(path, "/v1beta/models/") {
		_, _, err := relayconstant.ParseGeminiModelAction(path)
		return err == nil
	}
	return false
}
