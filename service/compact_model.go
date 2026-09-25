package service

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

func ChannelSupportsCompactEndpoint(channel *model.Channel, requestedModel string) bool {
	if channel == nil {
		return false
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI,
		constant.ChannelTypeAzure,
		constant.ChannelTypeCodex,
		constant.ChannelTypeNewAPI,
		constant.ChannelTypeSub2API, constant.ChannelTypeOllama:
		return true
	case constant.ChannelTypeAdvancedCustom, constant.ChannelTypeVLLM, constant.ChannelTypeSGLang:
		config := channel.GetOtherSettings().AdvancedCustom
		return config != nil && config.SupportsPathForModel("/v1/responses/compact", requestedModel)
	default:
		return false
	}
}

// ChannelSupportsNativeResponses reports whether a channel can receive the
// OpenAI Responses wire format without translating it to another protocol.
// Remote compaction state is meaningful only on these channels.
func ChannelSupportsNativeResponses(channel *model.Channel, requestedModel string) bool {
	if channel == nil {
		return false
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI,
		constant.ChannelTypeAzure,
		constant.ChannelTypeCodex,
		constant.ChannelTypeNewAPI,
		constant.ChannelTypeSub2API, constant.ChannelTypeOllama, constant.ChannelTypeZhipu_v4:
		return true
	case constant.ChannelTypeAdvancedCustom, constant.ChannelTypeVLLM, constant.ChannelTypeSGLang:
		config := channel.GetOtherSettings().AdvancedCustom
		return config != nil && config.SupportsNativeResponsesForModel(requestedModel)
	default:
		return false
	}
}
