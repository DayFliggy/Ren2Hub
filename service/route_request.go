package service

import (
	"slices"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
)

// LookupTaskPluginEndpoint resolves endpoint candidates from one immutable
// routing generation without decoding the request body or executing plugin JS.
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

// taskPluginIdentities lists the channel types and plugin keys that may serve
// expected; a built-in channel type serves every plugin that declares it.
func taskPluginIdentities(g *jsplugin.RoutingGeneration, candidates []*jsplugin.LoadedPlugin, expected string) ([]int, []string) {
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
			types = append(types, typ)
		}
	}
	if !found {
		return nil, nil
	}
	return types, keys
}

// taskPluginEndpointIdentities derives candidate channel types and plugin keys
// for shared task-plugin endpoints during dispatch.
func taskPluginEndpointIdentities(endpoint jsplugin.PinnedEndpoint, expected string) ([]int, []string) {
	if len(endpoint.Candidates) > 1 {
		plugins := make([]*jsplugin.LoadedPlugin, 0, len(endpoint.Candidates))
		for _, candidate := range endpoint.Candidates {
			plugins = append(plugins, candidate.Plugin)
		}
		if types, keys := taskPluginIdentities(endpoint.Generation, plugins, expected); len(keys) > 0 {
			return types, keys
		}
	}
	return taskPluginIdentities(endpoint.Generation, []*jsplugin.LoadedPlugin{endpoint.Plugin}, expected)
}
