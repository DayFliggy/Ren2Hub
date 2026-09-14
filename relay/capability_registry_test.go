package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanRequestForChannelRejectsUnavailableAndUnsafePaths(t *testing.T) {
	for _, tc := range []struct {
		name        string
		channelType int
		request     any
		wantError   bool
		target      types.RelayFormat
	}{
		{"Claude Responses", constant.ChannelTypeAnthropic, &dto.OpenAIResponsesRequest{Model: "claude-test", Input: []byte(`"hi"`)}, false, types.RelayFormatClaude},
		{"Codex rejects chat", constant.ChannelTypeCodex, &dto.GeneralOpenAIRequest{Model: "gpt-test"}, true, ""},
		{"Mistral rejects Gemini", constant.ChannelTypeMistral, &dto.GeminiChatRequest{}, true, ""},
		{"Claude rejects embeddings", constant.ChannelTypeAnthropic, &dto.EmbeddingRequest{Model: "test", Input: "hi"}, true, ""},
		{"native continuation", constant.ChannelTypeOpenAI, &dto.OpenAIResponsesRequest{Model: "gpt-test", PreviousResponseID: "resp_private"}, false, types.RelayFormatOpenAIResponses},
		{"converted continuation", constant.ChannelTypeGemini, &dto.OpenAIResponsesRequest{Model: "gemini-test", PreviousResponseID: "resp_private"}, true, ""},
		{"safe tool rejection", constant.ChannelTypeGemini, &dto.OpenAIResponsesRequest{Model: "gemini-test", Tools: []byte(`[{"type":"custom","name":"patch"}]`)}, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{Request: tc.request.(dto.Request), OriginModelName: "test", ConversionPolicy: types.ConversionLossPolicySafe}
			err := PlanRequestForChannel(nil, info, &model.Channel{Id: 1, Type: tc.channelType})
			if tc.wantError {
				require.Error(t, err)
				assert.Nil(t, info.ConversionPlan)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, info.ConversionPlan)
			assert.Equal(t, tc.target, info.ConversionPlan.To)
		})
	}
}

func TestSelectedConversionPlanIsExecutedAndAudited(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "gemini-test", Input: []byte(`"hi"`), Tools: []byte(`[{"type":"custom","name":"patch"}]`)}
	info := &relaycommon.RelayInfo{Request: req, OriginModelName: req.Model, ConversionPolicy: types.ConversionLossPolicyAllow}
	require.NoError(t, PlanRequestForChannel(nil, info, &model.Channel{Id: 1, Type: constant.ChannelTypeGemini}))
	result, err := service.ConvertRequest(nil, info, types.RelayFormatGemini, req)
	require.NoError(t, err)
	assert.Equal(t, info.ConversionPlan.RequestConverter, result.Converter)
	assert.NotEmpty(t, info.ConversionDiagnostics)
	req.PreviousResponseID = "resp_changed"
	_, err = service.ConvertRequest(nil, info, types.RelayFormatGemini, req)
	require.Error(t, err)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	assert.NotEmpty(t, loss.Diagnostics)
	_ = relayconvert.StreamOutcomeCompleted
}
