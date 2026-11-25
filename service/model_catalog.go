package service

import (
	"context"
	"runtime"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

type CatalogModel struct {
	model.Pricing
	model.CatalogPrice
}
type CatalogChannel struct {
	RouteChannel
	Lab    modellab.Classification `json:"lab"`
	Health *CatalogChannelHealth   `json:"health,omitempty"`
}
type CatalogOffer struct {
	ModelID                string                  `json:"model_id"`
	ChannelID              int                     `json:"channel_id"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	ChannelRatio           string                  `json:"channel_ratio"`
	model.CatalogPrice
	Health CatalogOfferHealth `json:"health"`
}
type ModelCatalog struct {
	SchemaVersion      int                            `json:"schema_version"`
	Models             []CatalogModel                 `json:"models"`
	Vendors            []model.PricingVendor          `json:"vendors"`
	SupportedEndpoints map[string]common.EndpointInfo `json:"supported_endpoints"`
	Channels           []CatalogChannel               `json:"channels"`
	Offers             []CatalogOffer                 `json:"offers"`
	ServerAddress      string                         `json:"server_address"`
	GeneratedAt        int64                          `json:"generated_at"`
}

var catalogAfterBuildOnce func()

func BuildModelCatalog(ctx context.Context, userID, tokenID int) (ModelCatalog, error) {
	for attempt := 0; attempt < catalogBuildMaxAttempts; attempt++ {
		versionBefore := routeObservations.catalogConfigVersion()
		if versionBefore%2 != 0 {
			runtime.Gosched()
			continue
		}
		result, err := buildModelCatalogOnce(ctx, userID, tokenID, time.Now())
		if err != nil {
			return result, err
		}
		if catalogAfterBuildOnce != nil {
			catalogAfterBuildOnce()
		}
		if versionAfter := routeObservations.catalogConfigVersion(); versionAfter == versionBefore {
			return result, nil
		}
	}
	return ModelCatalog{}, ErrCatalogConfigChanging
}

func buildModelCatalogOnce(ctx context.Context, userID, tokenID int, now time.Time) (ModelCatalog, error) {
	result := ModelCatalog{
		SchemaVersion:      1,
		Models:             []CatalogModel{},
		Channels:           []CatalogChannel{},
		Offers:             []CatalogOffer{},
		Vendors:            []model.PricingVendor{},
		SupportedEndpoints: map[string]common.EndpointInfo{},
		ServerAddress:      system_setting.ServerAddress,
		GeneratedAt:        now.Unix(),
	}
	channels, mappings, err := catalogRouteChannelsWithMappings(ctx, userID, tokenID, true)
	if err != nil {
		return result, err
	}
	pricing := model.GetPricing()
	vendors := model.GetVendors()
	byName := map[string]model.Pricing{}
	for _, item := range pricing {
		byName[item.ModelName] = item
	}
	requested := map[string]bool{}
	eligibleChannels := make([]RouteChannel, 0, len(channels))
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		if !channel.Eligible {
			continue
		}
		eligibleChannels = append(eligibleChannels, channel)
		ids = append(ids, channel.ID)
		for _, name := range channel.Models {
			requested[name] = true
		}
	}
	names := make([]string, 0, len(requested))
	for name := range requested {
		names = append(names, name)
	}
	sort.Strings(names)
	prices, err := model.CatalogPrices(ctx, names)
	if err != nil {
		return result, err
	}
	labs, err := model.ChannelLabs(ctx, ids)
	if err != nil {
		return result, err
	}
	catalogModels := map[string]*CatalogModel{}
	vendorIDs := map[int]bool{}
	for _, name := range names {
		item, ok := byName[name]
		if !ok {
			item, ok = byName[ratio_setting.RoutingMatchModelName(name)]
		}
		if !ok {
			continue
		}
		item.ModelName = name
		item.SupportedEndpointTypes = []constant.EndpointType{}
		projected := &CatalogModel{Pricing: item, CatalogPrice: prices[name]}
		catalogModels[name] = projected
		vendorIDs[item.VendorID] = true
	}
	for _, channel := range eligibleChannels {
		for _, name := range channel.Models {
			item := catalogModels[name]
			if item == nil {
				continue
			}
			endpoints := channel.SupportedEndpoints[name]
			for _, ep := range endpoints {
				found := false
				for _, existing := range item.SupportedEndpointTypes {
					if ep == existing {
						found = true
						break
					}
				}
				if !found {
					item.SupportedEndpointTypes = append(item.SupportedEndpointTypes, ep)
				}
				if info, ok := common.GetDefaultEndpointInfo(ep); ok {
					result.SupportedEndpoints[string(ep)] = info
				}
			}
			price, err := model.ScaleCatalogPrice(item.CatalogPrice, channel.ChannelRatio)
			if err != nil {
				return result, err
			}
			result.Offers = append(result.Offers, CatalogOffer{
				ModelID:                name,
				ChannelID:              channel.ID,
				SupportedEndpointTypes: endpoints,
				ChannelRatio:           channel.ChannelRatio,
				CatalogPrice:           price,
			})
		}
	}
	offerHealth, channelHealth := projectCatalogHealth(
		now,
		result.Offers,
		mappings,
		routeQualities.snapshot(),
		routeObservations.catalogBreakerOpenUntil,
		routeObservations.catalogCooldownUntil,
		routeObservations.catalogChannelPending,
	)
	for index := range result.Offers {
		key := catalogOfferKey{
			channelID: result.Offers[index].ChannelID,
			modelID:   result.Offers[index].ModelID,
		}
		result.Offers[index].Health = offerHealth[key]
	}
	for _, channel := range eligibleChannels {
		summary, hasOffers := channelHealth[channel.ID]
		if !hasOffers {
			continue
		}
		copySummary := summary
		result.Channels = append(result.Channels, CatalogChannel{
			RouteChannel: channel,
			Lab:          labs[channel.ID],
			Health:       &copySummary,
		})
	}
	for _, name := range names {
		if item := catalogModels[name]; item != nil {
			result.Models = append(result.Models, *item)
		}
	}
	for _, vendor := range vendors {
		if vendorIDs[vendor.ID] {
			result.Vendors = append(result.Vendors, vendor)
		}
	}
	sort.Slice(result.Vendors, func(i, j int) bool { return result.Vendors[i].ID < result.Vendors[j].ID })
	return result, nil
}
