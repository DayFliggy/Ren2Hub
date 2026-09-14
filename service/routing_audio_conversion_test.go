package service

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudioConversionPlanExecutablePaths(t *testing.T) {
	for _, test := range []struct {
		name        string
		channelType int
		path        string
		payload     string
		stream      bool
		allowed     bool
	}{
		{"cloudflare transcription", constant.ChannelCloudflare, "/v1/audio/transcriptions", `{}`, false, true},
		{"cloudflare speech", constant.ChannelCloudflare, "/v1/audio/speech", `{}`, false, false},
		{"cloudflare translation", constant.ChannelCloudflare, "/v1/audio/translations", `{}`, false, false},
		{"cloudflare streaming transcription", constant.ChannelCloudflare, "/v1/audio/transcriptions", `{"stream":true}`, true, false},
		{"cloudflare SSE", constant.ChannelCloudflare, "/v1/audio/transcriptions", `{"stream_format":"sse"}`, true, false},
		{"minimax speech", constant.ChannelTypeMiniMax, "/v1/audio/speech", `{}`, false, true},
		{"minimax SSE", constant.ChannelTypeMiniMax, "/v1/audio/speech", `{"stream_format":"sse"}`, true, true},
		{"minimax transcription", constant.ChannelTypeMiniMax, "/v1/audio/transcriptions", `{}`, false, false},
		{"volc speech", constant.ChannelTypeVolcEngine, "/v1/audio/speech", `{}`, false, true},
		{"volc SSE", constant.ChannelTypeVolcEngine, "/v1/audio/speech", `{"stream_format":"sse"}`, true, true},
		{"volc translation", constant.ChannelTypeVolcEngine, "/v1/audio/translations", `{}`, false, false},
		{"openai SSE", constant.ChannelTypeOpenAI, "/v1/audio/speech", `{"stream_format":"sse"}`, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requirements, err := analyzeRoutePreviewRequest(RouteProfilePreviewInput{Model: "audio-test", Path: test.path, Request: json.RawMessage(test.payload)})
			require.NoError(t, err)
			assert.Equal(t, test.stream, requirements.Stream)
			_, err = PlanChannelConversion(requirements, &model.Channel{Type: test.channelType}, test.path, "audio-test", types.ConversionLossPolicySafe)
			if test.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
