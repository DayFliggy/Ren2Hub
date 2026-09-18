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

func TestClaudeStreamFirstFramePreservesParallelTools(t *testing.T) {
	info := &convmeta.Values{}
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatClaude, ResponseStreamOptions{})
	require.NoError(t, err)
	var events []*dto.ClaudeResponse
	for _, raw := range []string{
		`{"id":"chat_parallel","model":"test-model","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo","arguments":"{\"value\":"}},{"index":1,"id":"call_b","type":"function","function":{"name":"echo","arguments":"{\"value\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"0}"}},{"index":1,"function":{"arguments":"false}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`,
	} {
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(raw), &chunk))
		results, err := ConvertStreamResponseChunk(context.Background(), info, state, &chunk)
		require.NoError(t, err)
		for _, result := range results {
			events = append(events, result.Value.(*dto.ClaudeResponse))
		}
	}
	ids := map[int]string{}
	arguments := map[int]string{}
	stops := map[int]int{}
	starts, ends := 0, 0
	for _, event := range events {
		switch event.Type {
		case "message_start":
			starts++
		case "content_block_start":
			require.NotNil(t, event.ContentBlock)
			assert.Equal(t, "tool_use", event.ContentBlock.Type)
			assert.Equal(t, "echo", event.ContentBlock.Name)
			ids[event.GetIndex()] = event.ContentBlock.Id
		case "content_block_delta":
			require.NotNil(t, event.Delta)
			require.NotNil(t, event.Delta.PartialJson)
			arguments[event.GetIndex()] += *event.Delta.PartialJson
		case "content_block_stop":
			stops[event.GetIndex()]++
		case "message_delta":
			require.NotNil(t, event.Usage)
			assert.Equal(t, 3, event.Usage.OutputTokens)
			require.NotNil(t, event.Delta.StopReason)
			assert.Equal(t, "tool_use", *event.Delta.StopReason)
		case "message_stop":
			ends++
		}
	}
	assert.Equal(t, 1, starts)
	assert.Equal(t, 1, ends)
	assert.Equal(t, map[int]string{0: "call_a", 1: "call_b"}, ids)
	assert.Equal(t, map[int]string{0: `{"value":0}`, 1: `{"value":false}`}, arguments)
	assert.Equal(t, map[int]int{0: 1, 1: 1}, stops)
	final, err := FinalizeStreamResponseWithOutcome(context.Background(), info, state, StreamOutcomeCompleted)
	require.NoError(t, err)
	assert.Empty(t, final)
}

func TestClaudeResponseConversionRejectsMultipleChoices(t *testing.T) {
	for _, indexes := range [][]int{{0, 1}, {1}} {
		chat := &dto.OpenAITextResponse{}
		chunk := &dto.ChatCompletionsStreamResponse{}
		for _, index := range indexes {
			chat.Choices = append(chat.Choices, dto.OpenAITextResponseChoice{Index: index, Message: dto.Message{Role: "assistant", Content: "choice"}, FinishReason: "stop"})
			text := "choice"
			chunk.Choices = append(chunk.Choices, dto.ChatCompletionsStreamResponseChoice{Index: index, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}})
		}
		_, err := ConvertResponse(context.Background(), nil, types.RelayFormatClaude, chat)
		require.ErrorContains(t, err, "cannot represent multiple choices")
		_, err = ConvertStreamResponse(context.Background(), nil, types.RelayFormatClaude, chunk)
		require.ErrorContains(t, err, "cannot represent multiple choices")
	}
}
