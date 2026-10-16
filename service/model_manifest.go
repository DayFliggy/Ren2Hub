package service

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type ModelManifestRequest struct {
	UserID            int
	TokenID           int
	ModelLimitEnabled bool
	ModelLimit        map[string]bool
}

// BuildModelManifest shares the channel authorization and capability filter
// used by routing. Unknown descriptive capabilities never deny a valid route.
func BuildModelManifest(ctx context.Context, input ModelManifestRequest) (dto.ModelManifest, error) {
	catalog, err := discoverModelRoutes(ctx, input)
	return catalog.manifest, err
}

type discoveredModelRoute struct {
	model    string
	channel  *model.Channel
	endpoint constant.EndpointType
	price    modelRoutePrice
}

type discoveredModelCatalog struct {
	manifest dto.ModelManifest
	routes   []discoveredModelRoute
	metadata map[string]model.Model
}

// discoverModelRoutes is shared by protocol model discovery and the console.
// It applies authorization, executable endpoint bindings and price eligibility
// before either caller projects any model or channel information.
func discoverModelRoutes(ctx context.Context, input ModelManifestRequest) (discoveredModelCatalog, error) {
	catalog := discoveredModelCatalog{
		manifest: dto.ModelManifest{SchemaVersion: 1, Models: []dto.ModelManifestEntry{}},
		metadata: make(map[string]model.Model),
	}
	manifest := &catalog.manifest
	query, err := availableRouteAbilities(ctx, input.UserID, input.TokenID)
	if err != nil {
		return catalog, err
	}
	var token model.Token
	if input.TokenID > 0 {
		if err := model.DB.WithContext(ctx).Select("id", "user_id", "status", "expired_time", "model_limits_enabled", "model_limits").
			Where("id = ? AND user_id = ?", input.TokenID, input.UserID).First(&token).Error; err != nil {
			return catalog, err
		}
		if token.Status != common.TokenStatusEnabled || (token.ExpiredTime != -1 && token.ExpiredTime < common.GetTimestamp()) {
			return catalog, model.ErrTokenInvalid
		}
	}
	tokenLimits := token.GetModelLimitsMap()
	var abilities []struct {
		Model       string
		ChannelID   int
		ChannelType int
		Status      int
	}
	if err := query.Select("abilities.model, abilities.channel_id, channels.type AS channel_type, channels.status").
		Order("COALESCE(abilities.priority, 0) DESC, abilities.weight DESC, abilities.channel_id ASC").Scan(&abilities).Error; err != nil {
		return catalog, err
	}
	channelIDs := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		channelIDs = append(channelIDs, ability.ChannelID)
	}
	sort.Ints(channelIDs)
	channelIDs = slices.Compact(channelIDs)
	if err := EnsureChannelCapabilities(ctx, channelIDs); err != nil {
		return catalog, err
	}
	capabilities, err := model.FindActiveChannelCapabilities(ctx, channelIDs, "", "")
	if err != nil {
		return catalog, err
	}
	type routeKey struct {
		channelID int
		model     string
	}
	byRoute := make(map[routeKey]model.ChannelModelCapability, len(capabilities))
	for _, capability := range capabilities {
		byRoute[routeKey{capability.ChannelID, capability.RequestModel}] = capability
	}
	if len(capabilities) == 0 {
		return catalog, nil
	}
	var channels []model.Channel
	if err := model.DB.WithContext(ctx).Select("id", "name", "type", "setting", "channel_ratio").Where("id IN ?", channelIDs).Find(&channels).Error; err != nil {
		return catalog, err
	}
	channelByID := make(map[int]*model.Channel, len(channels))
	for index := range channels {
		channelByID[channels[index].Id] = &channels[index]
	}
	priceNames := make([]string, 0, len(abilities)+len(capabilities))
	for _, ability := range abilities {
		priceNames = append(priceNames, ability.Model)
	}
	for _, capability := range capabilities {
		priceNames = append(priceNames, capability.ActualModel)
	}
	sort.Strings(priceNames)
	priceSnapshot, err := model.GetModelPricingSnapshot(slices.Compact(priceNames))
	if err != nil {
		return catalog, err
	}
	prices := make(map[string]model.ModelPricingEntry, len(priceSnapshot.Entries))
	for _, price := range priceSnapshot.Entries {
		prices[price.ModelName] = price
	}
	acceptUnpriced := operation_setting.SelfUseModeEnabled
	if !acceptUnpriced && input.UserID > 0 {
		settings, err := model.GetUserSetting(input.UserID, false)
		if err != nil {
			return catalog, err
		}
		acceptUnpriced = settings.AcceptUnsetRatioModel
	}
	generation := jsplugin.DefaultRegistry.Generation()
	byModel := make(map[string]*dto.ModelManifestEntry)
	for _, ability := range abilities {
		if token.ModelLimitsEnabled && !tokenAllowsRouteModel(tokenLimits, ability.Model) {
			continue
		}
		capability, exists := byRoute[routeKey{ability.ChannelID, modellab.NormalizeModel(ability.Model)}]
		if !exists {
			continue
		}
		var endpoints []constant.EndpointType
		if err := common.UnmarshalJsonStr(capability.EndpointTypes, &endpoints); err != nil {
			return catalog, fmt.Errorf("invalid route capability endpoints: %w", err)
		}
		for _, endpoint := range endpoints {
			path := ""
			if info, ok := common.GetDefaultEndpointInfo(endpoint); ok {
				path = strings.ReplaceAll(info.Path, "{model}", ability.Model)
			}
			filtered := filterRouteCapability(routeCapabilityFilterInput{
				Capability: capability, SnapshotVersion: capability.SnapshotVersion,
				ChannelStatus: ability.Status, ChannelType: ability.ChannelType, AbilityEnabled: true,
				TokenLimitEnabled: input.ModelLimitEnabled, TokenLimit: input.ModelLimit,
				RequestModel: ability.Model, NormalizedModel: modellab.NormalizeModel(ability.Model),
				RequestPath: path, EndpointType: string(endpoint), Entitled: true,
				RequireSnapshot: true, RequireEndpoint: true,
			})
			if filtered.Reason != "" {
				continue
			}
			for _, price := range modelRoutePrices(generation, channelByID[ability.ChannelID], ability.Model, capability.ActualModel, endpoint, prices) {
				if !acceptUnpriced && !price.configured {
					continue
				}
				entry := byModel[ability.Model]
				if entry == nil {
					entry = &dto.ModelManifestEntry{ID: ability.Model, Visible: true, OwnerChannelType: ability.ChannelType,
						PriceConfigured: price.configured, PriceVersion: price.version}
					byModel[ability.Model] = entry
				}
				if price.configured && !entry.PriceConfigured {
					entry.PriceConfigured, entry.PriceVersion = true, price.version
				}
				if !slices.Contains(entry.SupportedEndpointTypes, endpoint) {
					entry.SupportedEndpointTypes = append(entry.SupportedEndpointTypes, endpoint)
				}
				catalog.routes = append(catalog.routes, discoveredModelRoute{
					model: ability.Model, channel: channelByID[ability.ChannelID], endpoint: endpoint, price: price,
				})
			}
		}
	}
	if len(byModel) == 0 {
		return catalog, nil
	}
	names := make([]string, 0, len(byModel))
	for name := range byModel {
		names = append(names, name)
	}
	sort.Strings(names)
	// Capability cards describe exact models. A prefix metadata rule must not
	// accidentally claim the same context limit for all model generations.
	var metadata []model.Model
	if err := model.DB.WithContext(ctx).Where("model_name IN ? AND name_rule = ?", names, model.NameRuleExact).Find(&metadata).Error; err != nil {
		return catalog, err
	}
	disabled := make(map[string]bool)
	for _, meta := range metadata {
		entry := byModel[meta.ModelName]
		if entry == nil {
			// MySQL may use a case-insensitive collation. Capability cards
			// still require the exact configured public model identifier.
			continue
		}
		catalog.metadata[meta.ModelName] = meta
		entry.Capabilities = (*dto.ModelCapabilities)(meta.Capabilities)
		disabled[meta.ModelName] = meta.Status != 1
	}
	for _, name := range names {
		entry := byModel[name]
		if disabled[name] || (!acceptUnpriced && !entry.PriceConfigured) {
			continue
		}
		manifest.Models = append(manifest.Models, *entry)
	}
	return catalog, nil
}

type modelRoutePrice struct {
	usageExamples  []jsplugin.UsageExample
	configured     bool
	version        string
	values         model.PricingValues
	pluginKey      string
	pluginName     string
	usageSchema    map[string]jsplugin.UsageFieldSchema
	cacheWriteMode model.CacheWriteMode
	billingDetails model.LegacyBillingDetails
}

// A plugin price belongs only to its executable endpoint. Native endpoints use
// the same empty-plugin pricing scope as native request quoting.
func modelRoutePrices(generation *jsplugin.RoutingGeneration, channel *model.Channel, requested, actual string, endpoint constant.EndpointType, prices map[string]model.ModelPricingEntry) []modelRoutePrice {
	entry := prices[requested]
	var plugins []*jsplugin.LoadedPlugin
	if endpoint == constant.EndpointTypeTaskPlugin {
		plugins = generation.PluginsByModel(actual)
		if len(plugins) == 0 {
			plugins = generation.PluginsByModel(requested)
		}
	} else if info, known := common.GetDefaultEndpointInfo(endpoint); known {
		// Endpoint interception shares execution's model subsets and binding order.
		lookupModel := requested
		if declared, ok := generation.CanonicalModel(requested); ok {
			lookupModel = declared
		} else if alias, ok := model.ResolveTaskModelAlias(generation, requested); ok && alias.Declared != "" {
			lookupModel = alias.Declared
		}
		path := strings.ReplaceAll(info.Path, "{model}", requested)
		bindings := generation.LookupEndpointCandidates(info.Method, path, lookupModel)
		if len(bindings) > 0 {
			selected := SelectTaskPluginEndpointCandidate(channel, bindings)
			if selected == nil {
				return nil
			}
			plugins = []*jsplugin.LoadedPlugin{selected.Plugin}
		}
	}
	if len(plugins) == 0 {
		if endpoint == constant.EndpointTypeTaskPlugin || !TaskPluginChannelEligible(channel, "") {
			return nil
		}
		return []modelRoutePrice{{configured: model.ModelPriceConfigured(entry.Effective), version: entry.Version, values: entry.Effective,
			cacheWriteMode: entry.CacheWriteMode, billingDetails: entry.BillingDetails}}
	}
	results := make([]modelRoutePrice, 0, len(plugins))
	for _, plugin := range plugins {
		if !TaskPluginChannelEligible(channel, plugin.Meta.Key) {
			continue
		}
		schema := plugin.Meta.UsageFactsForModels(actual, requested)
		_, examples := plugin.Meta.UsageForModels(actual, requested)
		effective, _, err := model.ResolveTaskPricingValues(plugin.Meta.Key, schema, entry.Effective, prices[actual].Effective)
		if err != nil {
			continue
		}
		results = append(results, modelRoutePrice{
			configured: model.ModelPriceConfigured(effective), version: model.ModelPricingVersion(effective), values: effective,
			pluginKey: plugin.Meta.Key, pluginName: plugin.Meta.Name, usageSchema: schema, usageExamples: examples,
			cacheWriteMode: entry.CacheWriteMode, billingDetails: model.ResolveLegacyBillingDetails(requested, effective, entry.Configured),
		})
	}
	return results
}
