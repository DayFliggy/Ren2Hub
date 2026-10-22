package service

import (
	"context"
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
	Lab modellab.Classification `json:"lab"`
}
type CatalogOffer struct {
	ModelID                string                  `json:"model_id"`
	ChannelID              int                     `json:"channel_id"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	ChannelRatio           string                  `json:"channel_ratio"`
	model.CatalogPrice
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

func BuildModelCatalog(ctx context.Context, userID, tokenID int) (ModelCatalog, error) {
	result := ModelCatalog{SchemaVersion: 1, Models: []CatalogModel{}, Channels: []CatalogChannel{}, Offers: []CatalogOffer{}, Vendors: []model.PricingVendor{}, SupportedEndpoints: map[string]common.EndpointInfo{}, ServerAddress: system_setting.ServerAddress, GeneratedAt: time.Now().Unix()}
	channels, err := CatalogRouteChannels(ctx, userID, tokenID)
	if err != nil {
		return result, err
	}
	pricing, vendors, _, err := model.ReadPricing()
	if err != nil {
		return result, err
	}
	byName := map[string]model.Pricing{}
	for _, item := range pricing {
		byName[item.ModelName] = item
	}
	requested := map[string]bool{}
	ids := []int{}
	for _, channel := range channels {
		ids = append(ids, channel.ID)
		if channel.Eligible {
			for _, name := range channel.Models {
				requested[name] = true
			}
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
	for _, channel := range channels {
		result.Channels = append(result.Channels, CatalogChannel{RouteChannel: channel, Lab: labs[channel.ID]})
		if !channel.Eligible {
			continue
		}
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
			result.Offers = append(result.Offers, CatalogOffer{ModelID: name, ChannelID: channel.ID, SupportedEndpointTypes: endpoints, ChannelRatio: channel.ChannelRatio, CatalogPrice: price})
		}
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
