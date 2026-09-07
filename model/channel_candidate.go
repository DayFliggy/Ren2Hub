package model

import "github.com/QuantumNous/new-api/constant"

// ChannelSupportsRequestPath checks model-specific Advanced Custom routes.
func ChannelSupportsRequestPath(channel *Channel, requestPath, requestModel string) bool {
	if channel == nil {
		return false
	}
	if requestPath == "" || channel.Type != constant.ChannelTypeAdvancedCustom {
		return true
	}
	config := channel.GetOtherSettings().AdvancedCustom
	return config != nil && config.SupportsPathForModel(requestPath, requestModel)
}
