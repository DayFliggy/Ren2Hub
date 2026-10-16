package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiArgonUsesSharedThinkingRendererAcrossProtocols(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		request    any
	}{
		{"Chat", `{"model":"gemini-4-argon","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"medium"}`, &dto.GeneralOpenAIRequest{}},
		{"Responses", `{"model":"gemini-4-argon","input":"hello","reasoning":{"effort":"medium"}}`, &dto.OpenAIResponsesRequest{}},
		{"Claude", `{"model":"gemini-4-argon","messages":[{"role":"user","content":"hello"}],"thinking":{"type":"adaptive"},"output_config":{"effort":"medium"}}`, &dto.ClaudeRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(tc.body), tc.request))
			info := &convmeta.Values{OriginModelName: "gemini-4-argon", UpstreamModelName: "gemini-4-argon", Options: &convmeta.Options{Gemini: convmeta.GeminiOptions{ThinkingAdapterEnabled: true}}}
			result, err := ConvertRequest(context.Background(), info, types.RelayFormatGemini, tc.request)
			require.NoError(t, err)
			converted := result.Value.(*dto.GeminiChatRequest)
			require.NotNil(t, converted.GenerationConfig.ThinkingConfig)
			assert.Equal(t, "medium", converted.GenerationConfig.ThinkingConfig.ThinkingLevel)
			assert.Nil(t, converted.GenerationConfig.ThinkingConfig.ThinkingBudget)
			for _, d := range result.Diagnostics {
				assert.NotEqual(t, "gemini_unknown_capability", d.Code)
				assert.NotEqual(t, "gemini_level_adjusted", d.Code)
			}
		})
	}
}

func TestGeminiArgonNativeThinkingAndUsageStream(t *testing.T) {
	info := &convmeta.Values{OriginModelName: "gemini-4-argon", UpstreamModelName: "gemini-4-argon"}
	native := &dto.GeminiChatRequest{GenerationConfig: dto.GeminiChatGenerationConfig{ThinkingConfig: &dto.GeminiThinkingConfig{ThinkingLevel: "medium"}}}
	require.NoError(t, ApplyGeminiThinkingConfigChecked(native, info))
	assert.Equal(t, "medium", native.GenerationConfig.ThinkingConfig.ThinkingLevel)
	assert.Equal(t, "medium", info.GetReasoningEffort())
	stop := "STOP"
	chunk := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Thought: true, Text: "reason"}, {Text: "answer"}}}, FinishReason: &stop}}, UsageMetadata: dto.GeminiUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 3, ThoughtsTokenCount: 7, TotalTokenCount: 20}}
	state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	chunks, err := ConvertStreamResponseChunk(context.Background(), info, state, chunk)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	first := chunks[0].Value.(*dto.ChatCompletionsStreamResponse)
	assert.Equal(t, "reason", first.Choices[0].Delta.GetReasoningContent())
	assert.Equal(t, "answer", first.Choices[0].Delta.GetContentString())
	full, err := ConvertResponse(context.Background(), info, types.RelayFormatOpenAI, chunk)
	require.NoError(t, err)
	result := full.Value.(*dto.OpenAITextResponse)
	assert.Equal(t, 10, result.Usage.PromptTokens)
	assert.Equal(t, 10, result.Usage.CompletionTokens)
	assert.Equal(t, 7, result.Usage.CompletionTokenDetails.ReasoningTokens)
}
