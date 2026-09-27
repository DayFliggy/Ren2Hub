package service

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
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

// AvailableRoutePriceVersions resolves prices only for channels the caller can
// route to. A price attached to an inaccessible channel cannot grant visibility.
func AvailableRoutePriceVersions(ctx context.Context, userID, tokenID int) (map[string][]billing.PriceVersion, error) {
	query, err := availableRouteAbilities(ctx, userID, tokenID)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Model     string
		ChannelID int
	}
	if err := query.Distinct("abilities.model", "abilities.channel_id").Order("abilities.model, abilities.channel_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	prices := make(map[string][]billing.PriceVersion)
	seen := make(map[string]map[int64]bool)
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
			if seen[row.Model] == nil {
				seen[row.Model] = make(map[int64]bool)
			}
			if !seen[row.Model][version.ID] {
				prices[row.Model] = append(prices[row.Model], *version)
				seen[row.Model][version.ID] = true
			}
			break
		}
	}
	for _, versions := range prices {
		sort.Slice(versions, func(i, j int) bool { return versions[i].ChannelID < versions[j].ChannelID })
	}
	return prices, nil
}

func AvailableRoutePrices(ctx context.Context, userID int) ([]model.Pricing, error) {
	versionsByModel, err := AvailableRoutePriceVersions(ctx, userID, 0)
	if err != nil {
		return nil, err
	}
	prices := make([]model.Pricing, 0)
	for _, price := range model.GetPricing() {
		if versions := versionsByModel[price.ModelName]; len(versions) != 0 {
			price.PriceVersions = versions
			price.PriceVersion = &price.PriceVersions[0]
			price.BillingMode, price.BillingExpr = price.PriceVersion.Mode, price.PriceVersion.Expression
			prices = append(prices, price)
		}
	}
	sort.Slice(prices, func(i, j int) bool { return prices[i].ModelName < prices[j].ModelName })
	return prices, nil
}
