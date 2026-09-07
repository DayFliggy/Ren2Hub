package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterRouteCapabilityTreatsLowConfidenceResolutionAsUnknown(t *testing.T) {
	result := filterRouteCapability(routeCapabilityFilterInput{
		Capability: model.ChannelModelCapability{
			ChannelID:  1,
			LabSlug:    "",
			Confidence: 0.89,
			Source:     "unknown",
			State:      model.RouteCapabilityStateUnresolved,
		},
		ChannelStatus:  common.ChannelStatusEnabled,
		AbilityEnabled: true,
		Entitled:       true,
	})

	assert.Equal(t, RouteFilterUnknownCapability, result.Reason)
}

func TestMediaRouteCapabilityAdmission(t *testing.T) {
	for _, test := range []struct {
		name        string
		channelType int
		requestPath string
		allowed     bool
	}{
		{"openai_speech", constant.ChannelTypeOpenAI, "/v1/audio/speech", true},
		{"openai_transcription", constant.ChannelTypeOpenAI, "/v1/audio/transcriptions", true},
		{"siliconflow_speech", constant.ChannelTypeSiliconFlow, "/v1/audio/speech", true},
		{"siliconflow_rerank", constant.ChannelTypeSiliconFlow, "/v1/rerank", true},
		{"cohere_rerank", constant.ChannelTypeCohere, "/v1/rerank", true},
		{"minimax_speech", constant.ChannelTypeMiniMax, "/v1/audio/speech", true},
		{"minimax_transcription", constant.ChannelTypeMiniMax, "/v1/audio/transcriptions", false},
		{"volcengine_speech", constant.ChannelTypeVolcEngine, "/v1/audio/speech", true},
		{"volcengine_translation", constant.ChannelTypeVolcEngine, "/v1/audio/translations", false},
		{"midjourney", constant.ChannelTypeMidjourney, "/mj/submit/imagine", true},
		{"suno", constant.ChannelTypeSunoAPI, "/suno/submit/music", true},
		{"video", constant.ChannelTypeKling, "/v1/videos", true},
		{"video_cannot_handle_audio", constant.ChannelTypeKling, "/v1/audio/speech", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			channel := &model.Channel{Id: 1, Type: test.channelType}
			endpoints, err := common.Marshal(endpointTypesForModel(channel, modellab.ModelMatch{InputModel: "test-model", RealModel: "test-model"}))
			require.NoError(t, err)
			result := filterRouteCapability(routeCapabilityFilterInput{
				Capability: model.ChannelModelCapability{
					ChannelID: 1, LabSlug: "custom", Confidence: 1, Source: "channel_configuration",
					State: model.RouteCapabilityStateEligible, EndpointTypes: string(endpoints), SnapshotVersion: 1,
				},
				ChannelStatus: common.ChannelStatusEnabled, ChannelType: test.channelType,
				AbilityEnabled: true, Entitled: true, SnapshotVersion: 1, RequireSnapshot: true,
				RequestModel: "test-model", RequestPath: test.requestPath,
				EndpointType: endpointTypeForRequestPath(test.requestPath), RequireEndpoint: true,
			})
			if test.allowed {
				assert.Empty(t, result.Reason)
			} else {
				assert.Equal(t, RouteFilterPathUnsupported, result.Reason)
			}
		})
	}
}
