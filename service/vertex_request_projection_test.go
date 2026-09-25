package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexNameOnlyPlanMatchesExecution(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	original := settings.RemoveFunctionResponseIdEnabled
	t.Cleanup(func() { settings.RemoveFunctionResponseIdEnabled = original })
	settings.RemoveFunctionResponseIdEnabled = true
	channel := &model.Channel{Type: constant.ChannelTypeVertexAi, Key: "isolated-key"}
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			var native dto.GeminiChatRequest
			require.NoError(t, json.Unmarshal([]byte(`{"contents":[{"role":"model","parts":[{"functionCall":{"id":"call_a","name":"echo","args":{}}},{"functionCall":{"id":"call_b","name":"echo","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"call_b","name":"echo","response":{"value":false}}},{"functionResponse":{"id":"call_a","name":"echo","response":{"value":0}}}]}],"generationConfig":{"maxOutputTokens":4096}}`), &native))
			converted, err := relayconvert.ConvertRequest(context.Background(), &convmeta.Values{UpstreamModelName: "gemini-2.5-flash", ChannelMetaAttached: true}, format, &native)
			require.NoError(t, err)
			requirements, err := relayconvert.AnalyzeRequest(converted.Value)
			require.NoError(t, err)
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				plan, planErr := PlanChannelConversion(requirements, channel, "/v1/chat/completions", "gemini-2.5-flash", policy)
				require.Error(t, planErr)
				require.NotEmpty(t, plan.Diagnostics)
				assert.Equal(t, "tool_identity_required", plan.Diagnostics[len(plan.Diagnostics)-1].Code)
				info := &relaycommon.RelayInfo{ConversionPolicy: policy}
				_, executionErr := ProjectVertexGeminiRequest(info, &native)
				require.Error(t, executionErr)
				require.Len(t, info.ConversionDiagnostics, 1)
				assert.Equal(t, "tool_identity_required", info.ConversionDiagnostics[0].Code)
			}
			settings.RemoveFunctionResponseIdEnabled = false
			plan, err := PlanChannelConversion(requirements, channel, "/v1/chat/completions", "gemini-2.5-flash", types.ConversionLossPolicySafe)
			require.NoError(t, err)
			for _, diagnostic := range plan.Diagnostics {
				assert.NotEqual(t, "tool_identity_required", diagnostic.Code)
			}
			projected, err := ProjectVertexGeminiRequest(nil, &native)
			require.NoError(t, err)
			assert.Same(t, &native, projected)
			settings.RemoveFunctionResponseIdEnabled = true
		})
	}
}
