package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// AvailableRouteModels applies the same channel ownership boundary to model
// discovery as request routing. Billing groups do not grant model access.
func AvailableRouteModels(ctx context.Context, userID, tokenID int) ([]string, error) {
	query, err := availableRouteAbilities(ctx, userID, tokenID)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0)
	err = query.Distinct("abilities.model").Order("abilities.model").Pluck("abilities.model", &models).Error
	return models, err
}

func availableRouteAbilities(ctx context.Context, userID, tokenID int) (*gorm.DB, error) {
	if model.DB == nil {
		return nil, ErrRouteSelectionUnavailable
	}
	query := model.DB.WithContext(ctx).Table("abilities").
		Joins("JOIN channels ON channels.id = abilities.channel_id").
		Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled)
	if userID > 0 {
		revoked := model.DB.Model(&model.UserChannelEntitlement{}).Select("channel_id").
			Where("user_id = ? AND source = ?", userID, model.RouteSourcePlatform).
			Where("status <> ? OR revoked_at > 0 OR (expires_at > 0 AND expires_at <= ?)", model.RouteEntitlementStatusEnabled, common.GetTimestamp())
		query = query.Where("channels.id NOT IN (?)", revoked)
	}
	if tokenID > 0 {
		var profile model.UserRouteProfile
		err := model.DB.WithContext(ctx).Where("user_id = ? AND token_id = ?", userID, tokenID).First(&profile).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		if err == nil {
			if profile.Status != model.RouteProfileStatusEnabled {
				return query.Where("1 = 0"), nil
			}
			if profile.Mode == model.RouteModeManual {
				if profile.ActiveGroupID == nil {
					return query.Where("1 = 0"), nil
				}
				entries := model.DB.Model(&model.UserRouteEntry{}).Select("user_route_entries.channel_id").
					Joins("JOIN user_route_groups ON user_route_groups.id = user_route_entries.group_id").
					Where("user_route_groups.id = ? AND user_route_groups.profile_id = ? AND user_route_groups.enabled = ?", *profile.ActiveGroupID, profile.ID, true).
					Where("user_route_entries.enabled = ? AND user_route_entries.source = ?", true, model.RouteSourcePlatform)
				query = query.Where("channels.id IN (?)", entries)
			} else if profile.Mode != model.RouteModeAutoLab {
				return nil, ErrRouteProfileValidation
			}
		}
	}
	return query, nil
}

type routePrice struct {
	model      string
	version    billing.PriceVersion
	multiplier string
}

// A price attached to an inaccessible channel cannot grant visibility.
func availableRoutePrices(ctx context.Context, userID, tokenID int) ([]routePrice, error) {
	query, err := availableRouteAbilities(ctx, userID, tokenID)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Model        string
		ChannelID    int
		ChannelRatio string
	}
	if err := query.Distinct("abilities.model", "abilities.channel_id", "channels.channel_ratio").Order("abilities.model, abilities.channel_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	prices := make([]routePrice, 0)
	now := time.Now()
	for _, row := range rows {
		for _, candidate := range reasoning.BillingModelCandidates(row.Model) {
			version, err := model.GetActiveBillingPriceForChannel(ctx, candidate, row.ChannelID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if errors.Is(err, model.ErrAutomaticBillingPriceUnavailable) {
				break
			}
			if err != nil {
				return nil, err
			}
			if (version.EffectiveFrom != nil && now.Before(*version.EffectiveFrom)) || (version.EffectiveTo != nil && now.After(*version.EffectiveTo)) {
				break
			}
			ratio := row.ChannelRatio
			if version.ScopeKind == billing.PriceScopeChannel {
				ratio = "1"
			}
			ratio, err = model.NormalizeChannelRatio(ratio, false)
			if err != nil {
				return nil, err
			}
			prices = append(prices, routePrice{row.Model, *version, ratio})
			break
		}
	}
	return prices, nil
}

func AvailableRoutePriceVersions(ctx context.Context, userID, tokenID int) (map[string][]billing.PriceVersion, error) {
	rows, err := availableRoutePrices(ctx, userID, tokenID)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]billing.PriceVersion)
	seen := make(map[string]bool)
	for _, row := range rows {
		key := row.model + ":" + row.version.Hash
		if seen[key] {
			continue
		}
		seen[key] = true
		result[row.model] = append(result[row.model], row.version)
	}
	return result, nil
}

// PublicPriceVersion is a presentation projection, not a re-serialized frozen
// contract. Its hash identifies the original version; channel identity stays private.
type PublicPriceVersion struct {
	Metering           *billing.PriceMetering   `json:"metering,omitempty"`
	ID                 int64                    `json:"id"`
	Hash               string                   `json:"hash"`
	ContractVersion    int                      `json:"contract_version"`
	CalculationVersion string                   `json:"calculation_version"`
	ResultUnit         string                   `json:"result_unit"`
	ScopeKind          string                   `json:"scope_kind"`
	Model              string                   `json:"model"`
	Mode               string                   `json:"mode"`
	Prices             []billing.UnitPrice      `json:"prices"`
	FlatFee            string                   `json:"flat_fee,omitempty"`
	Expression         string                   `json:"expression,omitempty"`
	CreatedAt          int64                    `json:"created_at"`
	EffectiveFrom      *time.Time               `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time               `json:"effective_to,omitempty"`
	UsageScope         string                   `json:"usage_scope,omitempty"`
	UsageSchema        *billingexpr.UsageSchema `json:"usage_schema,omitempty"`
}

type PriceAdjustment struct {
	Kind       string `json:"kind"`
	Multiplier string `json:"multiplier"`
}

type RoutePriceOffer struct {
	OfferID        string              `json:"offer_id"`
	PricedModel    string              `json:"priced_model"`
	PriceVersion   PublicPriceVersion  `json:"price_version"`
	Conditions     map[string]any      `json:"conditions"`
	Adjustments    []PriceAdjustment   `json:"adjustments"`
	DisplayPrices  []billing.UnitPrice `json:"display_prices"`
	DisplayFlatFee string              `json:"display_flat_fee,omitempty"`
	Availability   map[string]string   `json:"availability"`
}

type RoutePricing struct {
	model.Pricing
	Offers []RoutePriceOffer `json:"offers"`
}

func AvailableRoutePrices(ctx context.Context, userID int) ([]RoutePricing, error) {
	rows, err := availableRoutePrices(ctx, userID, 0)
	if err != nil {
		return nil, err
	}
	offersByModel := make(map[string][]RoutePriceOffer)
	seen := make(map[string]bool)
	for _, row := range rows {
		version := row.version
		offerID, err := billing.Hash(struct{ Model, VersionHash, Multiplier string }{row.model, version.Hash, row.multiplier})
		if err != nil {
			return nil, err
		}
		if seen[offerID] {
			continue
		}
		seen[offerID] = true
		multiplier, err := decimal.NewFromString(row.multiplier)
		if err != nil {
			return nil, err
		}
		offer := RoutePriceOffer{
			OfferID: offerID, PricedModel: version.Model,
			PriceVersion: PublicPriceVersion{ID: version.ID, Hash: version.Hash, ContractVersion: version.ContractVersion, CalculationVersion: version.CalculationVersion, ResultUnit: version.ResultUnit, ScopeKind: version.ScopeKind, Model: version.Model, Mode: version.Mode, Prices: version.Prices, FlatFee: version.FlatFee, Expression: version.Expression, CreatedAt: version.CreatedAt, EffectiveFrom: version.EffectiveFrom, EffectiveTo: version.EffectiveTo},
			Conditions:   map[string]any{}, Adjustments: []PriceAdjustment{{Kind: "channel_multiplier", Multiplier: row.multiplier}},
			DisplayPrices: make([]billing.UnitPrice, 0, len(version.Prices)), Availability: map[string]string{"status": "available"},
		}
		if version.UsageScope != "" {
			offer.Conditions["usage_scope"] = version.UsageScope
		}
		offer.PriceVersion.UsageScope = version.UsageScope
		offer.PriceVersion.UsageSchema = version.UsageSchema
		offer.PriceVersion.Metering = version.Metering
		if version.Support != nil {
			offer.Conditions["dimensions"] = version.Support.Dimensions
			offer.Conditions["service_tiers"] = version.Support.ServiceTiers
		}
		if version.Expression != "" {
			offer.Conditions["context_rule"] = version.Expression
		}
		for _, line := range version.Prices {
			projected := line
			if len(line.Tiers) != 0 {
				projected.Tiers = make([]billing.PriceTier, 0, len(line.Tiers))
				for _, tier := range line.Tiers {
					amount, err := decimal.NewFromString(tier.UnitPrice)
					if err != nil {
						return nil, err
					}
					projected.Tiers = append(projected.Tiers, billing.PriceTier{UpTo: tier.UpTo, UnitPrice: amount.Mul(multiplier).String()})
				}
			} else {
				amount, err := decimal.NewFromString(line.Price)
				if err != nil {
					return nil, err
				}
				projected.Price = amount.Mul(multiplier).String()
			}
			offer.DisplayPrices = append(offer.DisplayPrices, projected)
		}
		if version.FlatFee != "" {
			amount, err := decimal.NewFromString(version.FlatFee)
			if err != nil {
				return nil, err
			}
			offer.DisplayFlatFee = amount.Mul(multiplier).String()
		}
		offersByModel[row.model] = append(offersByModel[row.model], offer)
	}
	prices := make([]RoutePricing, 0)
	for _, price := range model.GetPricing() {
		if offers := offersByModel[price.ModelName]; len(offers) != 0 {
			prices = append(prices, RoutePricing{Pricing: price, Offers: offers})
		}
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i].ModelName < prices[j].ModelName })
	return prices, nil
}
