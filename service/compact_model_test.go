package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestCompactChannelStageCapabilities(t *testing.T) {
	openAI := &model.Channel{Type: constant.ChannelTypeOpenAI}
	mapping := `{"gpt-5":"real-openai-compact"}`
	openAI.ModelMapping = &mapping
	require.True(t, compactChannelSupportsStage(openAI, map[string]bool{"gpt-5": true}, "gpt-5", "gpt-5-openai-compact", relaycommon.CompactAttemptExact))
	require.True(t, compactChannelSupportsStage(openAI, map[string]bool{"gpt-5": true}, "gpt-5", "gpt-5-openai-compact", relaycommon.CompactAttemptBase))

	unsupported := &model.Channel{Type: constant.ChannelTypeOpenRouter}
	require.False(t, compactChannelSupportsStage(unsupported, map[string]bool{"gpt-5-openai-compact": true}, "gpt-5", "gpt-5-openai-compact", relaycommon.CompactAttemptExact))
}

func TestNonGPTCompactChannelUsesOnlyBaseAbility(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Models: "claude-3-5-sonnet-openai-compact"}
	require.Equal(t, relaycommon.CompactAttemptNone, SpecificChannelCompactStage(channel, "claude-3-5-sonnet-openai-compact"))

	channel.Models = "claude-3-5-sonnet"
	require.Equal(t, relaycommon.CompactAttemptBase, SpecificChannelCompactStage(channel, "claude-3-5-sonnet-openai-compact"))
	require.False(t, compactChannelSupportsStage(
		channel,
		map[string]bool{"claude-3-5-sonnet-openai-compact": true},
		"claude-3-5-sonnet",
		"claude-3-5-sonnet",
		relaycommon.CompactAttemptExact,
	))
}

func TestAdvancedCustomCompactRequiresExplicitRoute(t *testing.T) {
	channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom}
	channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", UpstreamPath: "/v1/responses"}},
	}})
	require.False(t, channelSupportsCompactEndpoint(channel, "gpt-5"))

	channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{
		Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses/compact", UpstreamPath: "/v1/responses/compact"}},
	}})
	require.True(t, channelSupportsCompactEndpoint(channel, "gpt-5"))
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
