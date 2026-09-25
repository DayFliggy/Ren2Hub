package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSRequiresEnabledNativeChannel(t *testing.T) {
	require.False(t, ChannelSupportsResponsesWebSocket(nil, "gpt-5"))
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeCodex, constant.ChannelTypeSub2API, constant.ChannelTypeNewAPI} {
		channel := &model.Channel{Type: channelType}
		require.False(t, ChannelSupportsResponsesWebSocket(channel, "gpt-5"))
		channel.Setting = common.GetPointer(`{"responses_websocket_enabled":true}`)
		require.True(t, ChannelSupportsResponsesWebSocket(channel, "gpt-5"))
	}
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeAnthropic, constant.ChannelTypeTaskPlugin, constant.ChannelTypeOllama} {
		channel := &model.Channel{Type: channelType, Setting: common.GetPointer(`{"responses_websocket_enabled":true}`)}
		require.False(t, ChannelSupportsResponsesWebSocket(channel, "gpt-5"))
	}
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, Setting: common.GetPointer(`{"responses_websocket_enabled":true}`)}
	require.False(t, ChannelSupportsResponsesWebSocket(channel, "gpt-5"))
	for _, converter := range []string{"none", "openai_responses_to_openai_chat_completions"} {
		channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/responses", UpstreamPath: "/responses", Models: []string{"gpt-5"}, Converter: converter,
		}}}})
		require.Equal(t, converter == "none", ChannelSupportsResponsesWebSocket(channel, "gpt-5"))
		require.False(t, ChannelSupportsResponsesWebSocket(channel, "other-model"))
	}
}
