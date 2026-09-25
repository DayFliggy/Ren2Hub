package relayconvert

import (
	"context"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProviderReasoningIntentSurvivesOpenAIProjection(t *testing.T) {
	for _, test := range []struct {
		name, body, effort string
		request            any
	}{
		{"claude disabled", `{"model":"test","max_tokens":4096,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"}}`, "none", &dto.ClaudeRequest{}},
		{"claude effort", `{"model":"test","max_tokens":4096,"messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"max"}}`, "max", &dto.ClaudeRequest{}},
		{"gemini effort", `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"xhigh"}}}`, "xhigh", &dto.GeminiChatRequest{}},
		{"gemini disabled", `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0,"includeThoughts":false}}}`, "none", &dto.GeminiChatRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, test.request)
			require.NoError(t, err)
			chat := result.Value.(*dto.GeneralOpenAIRequest)
			assert.Equal(t, test.effort, chat.ReasoningEffort)
			require.NotNil(t, chat.ReasoningConversion)
		})
	}
}
func TestReasoningWireLossStillRespectsStrictPolicy(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`"hello"`), Reasoning: &dto.Reasoning{Effort: "high", Summary: "auto"}}
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, req)
	require.NoError(t, err)
	chat := result.Value.(*dto.GeneralOpenAIRequest)
	require.NotNil(t, chat.ReasoningConversion)
	require.NotNil(t, chat.ReasoningConversion.IncludeThoughts)
	assert.True(t, *chat.ReasoningConversion.IncludeThoughts)
	req.Metadata = []byte(`{"tenant":"x"}`)
	_, err = ConvertRequest(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyStrict}}, types.RelayFormatGemini, req)
	require.Error(t, err)
}
