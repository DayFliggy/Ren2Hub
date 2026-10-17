package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// Selection uses the upstream group/model qualification and request filters;
// only the ordering differs. An empty private list must never enter random selection.
func getOrderedTokenRouteChannel(group, modelName string, ordered []int, filters []dto.ChannelFilter) (*Channel, error) {
	if len(ordered) == 0 {
		return nil, nil
	}
	lookups := []string{modelName}
	if normalized := ratio_setting.RoutingMatchModelName(modelName); normalized != modelName {
		lookups = append(lookups, normalized)
	}
	if common.MemoryCacheEnabled {
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		for _, lookup := range lookups {
			ids, _ := filterCandidateIDs(group2model2channels[group][lookup], modelName, filters)
			eligible := make(map[int]bool, len(ids))
			for _, id := range ids {
				eligible[id] = true
			}
			for _, id := range ordered {
				if channel := channelsIDM[id]; eligible[id] && channel != nil && channel.Status == common.ChannelStatusEnabled {
					return channel, nil
				}
			}
		}
		return nil, nil
	}
	for _, lookup := range lookups {
		var abilities []Ability
		if err := DB.Where(map[string]any{"group": group, "model": lookup, "enabled": true, "channel_id": ordered}).Find(&abilities).Error; err != nil {
			return nil, err
		}
		ids := make([]int, 0, len(abilities))
		for _, ability := range abilities {
			ids = append(ids, ability.ChannelId)
		}
		if len(ids) == 0 {
			continue
		}
		var channels []Channel
		if err := DB.Where("id IN ? AND status = ?", ids, common.ChannelStatusEnabled).Find(&channels).Error; err != nil {
			return nil, err
		}
		byID := make(map[int]*Channel, len(channels))
		for index := range channels {
			channel := &channels[index]
			if ok, _ := ChannelSatisfiesFilters(channel, modelName, filters); ok {
				byID[channel.Id] = channel
			}
		}
		for _, id := range ordered {
			if channel := byID[id]; channel != nil {
				return channel, nil
			}
		}
	}
	return nil, nil
}
