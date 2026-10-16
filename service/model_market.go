package service

import (
	"context"
	"slices"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/types"
)

// BuildModelMarket projects the same eligible routes as model discovery. It
// never creates billing evidence or changes the administrator's configuration.
func BuildModelMarket(ctx context.Context, userID int) (dto.ModelMarket, error) {
	result := dto.ModelMarket{
		SchemaVersion: 1, Models: []dto.MarketModel{}, Channels: []dto.MarketChannel{}, Offers: []dto.MarketOffer{},
		Telemetry:          dto.MarketTelemetry{Status: "unavailable", Reason: "channel_metrics_not_collected"},
		SupportedEndpoints: make(map[string]dto.MarketEndpoint),
	}
	if userID <= 0 {
		return result, ErrRouteProfileForbidden
	}
	catalog, err := discoverModelRoutes(ctx, ModelManifestRequest{UserID: userID})
	if err != nil {
		return result, err
	}
	modelIndexes := make(map[string]int, len(catalog.manifest.Models))
	vendorIDs := make([]int, 0)
	for _, entry := range catalog.manifest.Models {
		meta := catalog.metadata[entry.ID]
		modelIndexes[entry.ID] = len(result.Models)
		result.Models = append(result.Models, dto.MarketModel{
			ID: entry.ID, Description: meta.Description, Icon: meta.Icon, Capabilities: entry.Capabilities,
			SupportedEndpointTypes: entry.SupportedEndpointTypes, Baselines: []dto.MarketPricing{},
		})
		for _, endpoint := range entry.SupportedEndpointTypes {
			if info, exists := common.GetDefaultEndpointInfo(endpoint); exists {
				result.SupportedEndpoints[string(endpoint)] = dto.MarketEndpoint{Path: info.Path, Method: info.Method}
			}
		}
		if meta.VendorID > 0 {
			vendorIDs = append(vendorIDs, meta.VendorID)
		}
	}
	if len(vendorIDs) > 0 {
		var vendors []model.Vendor
		if err := model.DB.WithContext(ctx).Select("id", "name").Where("id IN ?", vendorIDs).Find(&vendors).Error; err != nil {
			return result, err
		}
		byID := make(map[int]string, len(vendors))
		for _, vendor := range vendors {
			byID[vendor.Id] = vendor.Name
		}
		for index := range result.Models {
			result.Models[index].Vendor = byID[catalog.metadata[result.Models[index].ID].VendorID]
		}
	}
	channels := make(map[int]dto.MarketChannel)
	pricingIndexes := make(map[string]int)
	offerIndexes := make(map[string]int)
	for _, route := range catalog.routes {
		modelIndex, visible := modelIndexes[route.model]
		if !visible {
			continue
		}
		channel := route.channel
		if _, exists := channels[channel.Id]; !exists {
			ratio, err := model.NormalizeChannelRatio(channel.ChannelRatio, false)
			if err != nil {
				return result, err
			}
			channels[channel.Id] = dto.MarketChannel{ID: channel.Id, Name: channel.Name, Ratio: ratio}
		}
		price := route.price
		details := marketBillingDetailsForRoute(route)
		// The same configured per-call amount is per-image on native image
		// endpoints, but per-request on text endpoints and per-task for plugins.
		// Native audio/realtime also excludes text-only cache/image dimensions.
		// Keep its endpoint scope separate even when all amounts are equal.
		pricingID, err := billing.Hash([]any{route.model, price.pluginKey, price.version, price.usageSchema, price.usageExamples, details, marketNativeAudioRoute(route)})
		if err != nil {
			return result, err
		}
		entry := &result.Models[modelIndex]
		pricingIndex, exists := pricingIndexes[pricingID]
		if !exists {
			pricingIndex = len(entry.Baselines)
			pricingIndexes[pricingID] = pricingIndex
			values := model.PricingValuesForAPI(price.values)
			if values == nil {
				values = model.PricingValues{}
			}
			// This baseline represents one executable scope, not other plugins'
			// possibly inaccessible overrides stored on the same model.
			delete(values, billing_setting.PluginBillingExprOption)
			usageSchema := make(map[string]any, len(price.usageSchema))
			for name, field := range price.usageSchema {
				usageSchema[name] = field
			}
			var reference *types.UsageReference
			if price.pluginKey != "" && values["billing_setting.billing_mode"] == "tiered_expr" {
				reference = taskUsageReference(route.model, price.pluginKey, price.pluginName, price.values, price.usageSchema, price.usageExamples)
			}
			entry.Baselines = append(entry.Baselines, dto.MarketPricing{
				ID: pricingID, Version: price.version, Configured: price.configured, Values: values, UsageReference: reference,
				PluginKey: price.pluginKey, PluginName: price.pluginName, UsageSchema: usageSchema,
				CacheWriteMode: string(price.cacheWriteMode), BillingDetails: details,
			})
		}
		baseline := &entry.Baselines[pricingIndex]
		if !slices.Contains(baseline.SupportedEndpointTypes, route.endpoint) {
			baseline.SupportedEndpointTypes = append(baseline.SupportedEndpointTypes, route.endpoint)
		}
		offerID := strconv.Itoa(channel.Id) + ":" + pricingID
		offerIndex, exists := offerIndexes[offerID]
		if !exists {
			offerIndex = len(result.Offers)
			offerIndexes[offerID] = offerIndex
			result.Offers = append(result.Offers, dto.MarketOffer{
				ID: offerID, ModelID: route.model, ChannelID: channel.Id, PricingID: pricingID,
			})
		}
		offer := &result.Offers[offerIndex]
		if !slices.Contains(offer.SupportedEndpointTypes, route.endpoint) {
			offer.SupportedEndpointTypes = append(offer.SupportedEndpointTypes, route.endpoint)
		}
	}
	for _, channel := range channels {
		result.Channels = append(result.Channels, channel)
	}
	sort.Slice(result.Channels, func(i, j int) bool { return result.Channels[i].ID < result.Channels[j].ID })
	return result, nil
}

func marketBillingDetailsForRoute(route discoveredModelRoute) dto.MarketBillingDetails {
	details := marketBillingDetails(route.price.billingDetails)
	_, perCall := route.price.values["ModelPrice"]
	if route.price.values["billing_setting.billing_mode"] == billing_setting.BillingModeTieredExpr {
		return details
	}
	if !perCall {
		if _, ratioPrice := route.price.values["ModelRatio"]; ratioPrice && marketNativeAudioRoute(route) {
			details.AudioTextBranches = true
		}
		return details
	}
	// Native image settlement records the generated image quantity regardless
	// of the public model name. Task plugins supply their own usage facts and
	// must not inherit native image counting merely by binding the same URL.
	details.ImageCount = route.price.pluginKey == "" && route.endpoint == constant.EndpointTypeImageGeneration
	if !details.ImageCount {
		details.RequestRules = nil
	}
	return details
}

func marketNativeAudioRoute(route discoveredModelRoute) bool {
	return route.price.pluginKey == "" &&
		(route.endpoint == constant.EndpointTypeAudio || route.endpoint == constant.EndpointTypeRealtime)
}

// Legacy display details are derived by the existing pricing resolver. Their
// public representation keeps rule multipliers as decimal text. Audio amounts
// are derived from the exact effective price fields, not these legacy floats.
func marketBillingDetails(details model.LegacyBillingDetails) dto.MarketBillingDetails {
	result := dto.MarketBillingDetails{ImageCount: details.ImageCount, AudioTextBranches: details.AudioTextBranches}
	for _, rule := range details.RequestRules {
		result.RequestRules = append(result.RequestRules, dto.MarketPricingRule{
			Condition: rule.Condition, Multiplier: strconv.FormatFloat(rule.Multiplier, 'f', -1, 64),
		})
	}
	return result
}
