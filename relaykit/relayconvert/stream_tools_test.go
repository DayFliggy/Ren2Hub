package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiToolStreamPreservesZeroArgumentsForSameNameParallelCalls(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
	require.NoError(t, err)
	for _, raw := range []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_b","type":"function","function":{"name":"echo","arguments":"{\"value\":"}},{"index":0,"id":"call_a","type":"function","function":{"name":"echo","arguments":"{\"value\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"0}"}},{"index":1,"function":{"arguments":"false}"}}]}}]}`,
	} {
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(raw), &chunk))
		results, err := ConvertStreamResponseChunk(context.Background(), nil, state, &chunk)
		require.NoError(t, err)
		for _, result := range results {
			for _, candidate := range result.Value.(*dto.GeminiChatResponse).Candidates {
				assert.Empty(t, candidate.Content.Parts, "partial JSON must not reach a Gemini tool executor")
			}
		}
	}
	finish := "tool_calls"
	results, err := ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ChatCompletionsStreamResponse{
		Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &finish}},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	parts := results[0].Value.(*dto.GeminiChatResponse).Candidates[0].Content.Parts
	require.Len(t, parts, 2)
	assert.Equal(t, "call_a", parts[0].FunctionCall.ID)
	assert.Equal(t, "echo", parts[0].FunctionCall.FunctionName)
	assert.Equal(t, map[string]any{"value": float64(0)}, parts[0].FunctionCall.Arguments)
	assert.Equal(t, "call_b", parts[1].FunctionCall.ID)
	assert.Equal(t, map[string]any{"value": false}, parts[1].FunctionCall.Arguments)
}

func TestOpenAIToolArgumentDeltaOmitsIdentityFields(t *testing.T) {
	chunk := &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
		ToolCalls: []dto.ToolCallResponse{{ID: "call_a", Type: "function", Function: dto.FunctionResponse{Name: "echo", Arguments: `{"value":0}`}}},
	}}}}
	chunk.ClearToolCalls()
	encoded, err := kitutil.Marshal(chunk)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, kitutil.Unmarshal(encoded, &payload))
	tool := payload["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)["tool_calls"].([]any)[0].(map[string]any)
	assert.NotContains(t, tool, "id")
	assert.NotContains(t, tool, "type", "null overwrites the official SDK's accumulated tool type")
	assert.NotContains(t, tool["function"], "name")
	assert.Equal(t, `{"value":0}`, tool["function"].(map[string]any)["arguments"])
}
