package service

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/require"
)

// Route planning rejects a tool schema that Gemini's typed Schema cannot hold
// only where the upstream needs that Schema: Gemini API v1. Other Gemini
// upstreams receive it unchanged in parametersJsonSchema (NewAPI c10780e05).
func TestGeminiChannelPlanFollowsTheJSONSchemaCapability(t *testing.T) {
	settings := model_setting.GetGeminiSettings()
	original := settings.VersionSettings
	t.Cleanup(func() {
		settings.VersionSettings = original
	})
	settings.VersionSettings = map[string]string{"default": "v1beta", "gemini-v1-model": "v1"}

	request := &dto.GeneralOpenAIRequest{
		Model:    "gemini-model",
		Messages: []dto.Message{{Role: "user", Content: "hi"}},
		Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "set_mode",
			Parameters: map[string]any{"type": "object", "properties": map[string]any{"mode": map[string]any{"const": "fast"}}}}}},
	}
	requirements, err := relayconvert.AnalyzeRequest(request)
	require.NoError(t, err)
	channel := &model.Channel{Type: constant.ChannelTypeGemini}

	_, err = PlanChannelConversion(requirements, channel, "/v1/chat/completions", "gemini-3.7-flash", types.ConversionLossPolicySafe)
	require.NoError(t, err)
	_, err = PlanChannelConversion(requirements, channel, "/v1/chat/completions", "gemini-v1-model", types.ConversionLossPolicySafe)
	require.ErrorContains(t, err, "tools[0].function.parameters.properties.mode.const")
	_, err = PlanChannelConversion(requirements, channel, "/v1/chat/completions", "gemini-v1-model", types.ConversionLossPolicyAllow)
	require.NoError(t, err)
}
