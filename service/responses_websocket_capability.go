package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

func ChannelSupportsResponsesWebSocket(channel *model.Channel, requestedModel string) bool {
	if channel == nil || !channel.GetSetting().ResponsesWebSocketEnabled {
		return false
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeCodex, constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI:
		return true
	case constant.ChannelTypeAdvancedCustom:
		config := channel.GetOtherSettings().AdvancedCustom
		if config == nil {
			return false
		}
		route, exists := config.MatchPathForModel("/v1/responses", requestedModel)
		return exists && route.IsNative()
	default:
		return false
	}
}
