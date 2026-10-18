package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// RouteChannelRows is shared by dispatch and public projections. Querying all
// matching rows before filtering prevents an arbitrary top-k from hiding a route.
func RouteChannelRows(ctx context.Context, modelName string, filters []dto.ChannelFilter) ([]*Channel, error) {
	rows := []*Channel{}
	q := DB.WithContext(ctx).Model(&Channel{})
	if modelName != "" {
		names := []string{modelName}
		if normalized := ratio_setting.RoutingMatchModelName(modelName); normalized != modelName {
			names = append(names, normalized)
		}
		var abilities []Ability
		if err := DB.WithContext(ctx).Select("channel_id").Where("model IN ? AND enabled = ?", names, true).Find(&abilities).Error; err != nil {
			return nil, err
		}
		if len(abilities) == 0 {
			return rows, nil
		}
		ids := make([]int, 0, len(abilities))
		for _, ability := range abilities {
			ids = append(ids, ability.ChannelId)
		}
		q = q.Where("id IN ?", ids)
	}
	if err := q.Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	if modelName == "" {
		return rows, nil
	}
	result := make([]*Channel, 0, len(rows))
	for _, c := range rows {
		if c.Status == common.ChannelStatusEnabled {
			if ok, _ := ChannelSatisfiesFilters(c, modelName, filters); ok {
				result = append(result, c)
			}
		}
	}
	return result, nil
}

// EnabledRouteModels projects the same enabled ability rows used by dispatch.
// It is a single batch lookup, including when the catalog spans many channels.
func EnabledRouteModels(ctx context.Context, channelIDs []int) (map[int][]string, error) {
	result := map[int][]string{}
	if len(channelIDs) == 0 {
		return result, nil
	}
	var rows []Ability
	if err := DB.WithContext(ctx).Select("channel_id", "model").Where("channel_id IN ? AND enabled = ?", channelIDs, true).Order("model asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ChannelId] = append(result[row.ChannelId], row.Model)
	}
	return result, nil
}
