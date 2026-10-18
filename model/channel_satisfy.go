package model

import "github.com/QuantumNous/new-api/setting/ratio_setting"

func CheckChannelEnabledForModel(modelName string, channelID int) (bool, error) {
	if modelName == "" || channelID <= 0 {
		return false, nil
	}
	names := []string{modelName}
	if normalized := ratio_setting.RoutingMatchModelName(modelName); normalized != modelName {
		names = append(names, normalized)
	}
	var count int64
	err := DB.Model(&Ability{}).Where("model IN ? AND channel_id = ? AND enabled = ?", names, channelID, true).Count(&count).Error
	return count > 0, err
}
