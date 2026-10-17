package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func IsChannelEnabledForGroupModel(group string, modelName string, channelID int) bool {
	enabled, _ := CheckChannelEnabledForGroupModel(group, modelName, channelID)
	return enabled
}

// CheckChannelEnabledForGroupModel preserves lookup failures for callers that
// must distinguish unavailable configuration from a denied channel.
func CheckChannelEnabledForGroupModel(group string, modelName string, channelID int) (bool, error) {
	if group == "" || modelName == "" || channelID <= 0 {
		return false, nil
	}
	if !common.MemoryCacheEnabled {
		return isChannelEnabledForGroupModelDB(group, modelName, channelID)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	if group2model2channels == nil {
		return false, nil
	}

	if isChannelIDInList(group2model2channels[group][modelName], channelID) {
		return true, nil
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	if normalized != "" && normalized != modelName {
		return isChannelIDInList(group2model2channels[group][normalized], channelID), nil
	}
	return false, nil
}

func IsChannelEnabledForAnyGroupModel(groups []string, modelName string, channelID int) bool {
	if len(groups) == 0 {
		return false
	}
	for _, g := range groups {
		if IsChannelEnabledForGroupModel(g, modelName, channelID) {
			return true
		}
	}
	return false
}

func isChannelEnabledForGroupModelDB(group string, modelName string, channelID int) (bool, error) {
	var count int64
	err := DB.Model(&Ability{}).
		Where(map[string]any{"group": group, "model": modelName, "channel_id": channelID, "enabled": true}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	normalized := ratio_setting.RoutingMatchModelName(modelName)
	if normalized == "" || normalized == modelName {
		return false, nil
	}
	count = 0
	err = DB.Model(&Ability{}).
		Where(map[string]any{"group": group, "model": normalized, "channel_id": channelID, "enabled": true}).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func isChannelIDInList(list []int, channelID int) bool {
	return slices.Contains(list, channelID)
}
