package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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

// AvailableRoutePrices publishes the range of currently available channel
// multipliers without disclosing channel identities or credentials.
func AvailableRoutePrices(ctx context.Context, userID int) ([]model.Pricing, error) {
	query, err := availableRouteAbilities(ctx, userID, 0)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Model        string
		ChannelRatio float64
	}
	if err := query.Distinct("abilities.model", "channels.channel_ratio").Scan(&rows).Error; err != nil {
		return nil, err
	}
	type ratioRange struct{ min, max float64 }
	ranges := make(map[string]ratioRange)
	for _, row := range rows {
		channel := model.Channel{ChannelRatio: row.ChannelRatio}
		_, ratio, err := channel.RoutingSettings()
		if err != nil {
			return nil, err
		}
		for _, name := range ExpandCompactPermissionModels([]string{row.Model}) {
			current, exists := ranges[name]
			if !exists {
				current = ratioRange{ratio, ratio}
			}
			current.min, current.max = min(current.min, ratio), max(current.max, ratio)
			ranges[name] = current
		}
	}
	prices := make([]model.Pricing, 0)
	for _, price := range model.GetPricing() {
		if ratios, ok := ranges[price.ModelName]; ok {
			price.ChannelRatioMin, price.ChannelRatioMax = ratios.min, ratios.max
			prices = append(prices, price)
		}
	}
	return prices, nil
}
