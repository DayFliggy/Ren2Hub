package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func TestExplicitZeroOutputLimitAcrossProtocols(t *testing.T) {
	zero, eight := uint(0), uint(8)
	requests := map[string]any{
		"chat":                       &dto.GeneralOpenAIRequest{Model: "test", MaxTokens: &zero, Messages: []dto.Message{{Role: "user", Content: "hello"}}},
		"chat_completion_precedence": &dto.GeneralOpenAIRequest{Model: "test", MaxTokens: &eight, MaxCompletionTokens: &zero, Messages: []dto.Message{{Role: "user", Content: "hello"}}},
		"responses":                  &dto.OpenAIResponsesRequest{Model: "test", MaxOutputTokens: &zero, Input: []byte(`"hello"`)},
	}
	var claude dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_tokens":0,"messages":[{"role":"user","content":"hello"}]}`), &claude))
	requests["claude"] = &claude
	var gemini dto.GeminiChatRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":0}}`), &gemini))
	requests["gemini"] = &gemini
	for source, request := range requests {
		for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
			t.Run(source+"/"+string(target), func(t *testing.T) {
				meta := claudeDefaultsMeta(func(string) int { return 512 }).(*convmeta.Values)
				meta.OriginModelName = "test"
				meta.UpstreamModelName = "test"
				meta.ChannelMetaAttached = true
				result, err := ConvertRequest(context.Background(), meta, target, request)
				require.NoError(t, err)
				body, err := kitutil.Marshal(result.Value)
				require.NoError(t, err)
				var fields map[string]any
				require.NoError(t, kitutil.Unmarshal(body, &fields))
				key := "max_tokens"
				switch target {
				case types.RelayFormatOpenAI:
					if _, present := fields["max_completion_tokens"]; present {
						key = "max_completion_tokens"
					}
				case types.RelayFormatOpenAIResponses:
					key = "max_output_tokens"
				case types.RelayFormatGemini:
					fields = fields["generationConfig"].(map[string]any)
					key = "maxOutputTokens"
				}
				require.Contains(t, fields, key)
				require.EqualValues(t, 0, fields[key])
			})
		}
	}
}

func TestChatToGeminiPreservesExplicitZeroSeed(t *testing.T) {
	zero := float64(0)
	result, err := ConvertRequest(context.Background(), &convmeta.Values{}, types.RelayFormatGemini, &dto.GeneralOpenAIRequest{
		Model: "test", Seed: &zero, Messages: []dto.Message{{Role: "user", Content: "hello"}},
	})
	require.NoError(t, err)
	request, ok := result.Value.(*dto.GeminiChatRequest)
	require.True(t, ok)
	require.NotNil(t, request.GenerationConfig.Seed)
	require.Zero(t, *request.GenerationConfig.Seed)
}
