package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestCompactEndpointCapabilityIsIndependentOfModelName(t *testing.T) {
	for _, name := range []string{"gpt-5", "claude-3-5-sonnet", "literal-openai-compact"} {
		require.True(t, ChannelSupportsCompactEndpoint(&model.Channel{Type: constant.ChannelTypeOpenAI}, name))
		require.False(t, ChannelSupportsCompactEndpoint(&model.Channel{Type: constant.ChannelTypeGemini}, name))
	}
}

func TestAdvancedCustomCompactRequiresExplicitRoute(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", UpstreamPath: "/v1/responses"}},
	}})
	require.False(t, ChannelSupportsCompactEndpoint(channel, "gpt-5"))

	channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses/compact", UpstreamPath: "/v1/responses/compact"}},
	}})
	require.True(t, ChannelSupportsCompactEndpoint(channel, "gpt-5"))
}

func TestNativeResponsesCapability(t *testing.T) {
	require.True(t, ChannelSupportsNativeResponses(&model.Channel{Type: constant.ChannelTypeOpenAI}, "gpt-5"))
	require.False(t, ChannelSupportsNativeResponses(&model.Channel{Type: constant.ChannelTypeGemini}, "gpt-5"))

	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/responses",
			Converter:    "none",
			Models:       []string{"gpt-5"},
		}},
	}})
	require.True(t, ChannelSupportsNativeResponses(channel, "gpt-5"))

	channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/responses",
			UpstreamPath: "/chat/completions",
			Converter:    "openai_responses_to_openai_chat_completions",
			Models:       []string{"gpt-5"},
		}},
	}})
	require.False(t, ChannelSupportsNativeResponses(channel, "gpt-5"))
}
