package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Embedding only the required interface deliberately hides optional providers.
type metaWithoutGeminiState struct{ convmeta.Meta }

func TestOpenAIToGeminiStreamOwnsParallelToolState(t *testing.T) {
	for name, info := range map[string]convmeta.Meta{
		"nil":                     nil,
		"required interface only": metaWithoutGeminiState{&convmeta.Values{}},
		"values":                  &convmeta.Values{},
	} {
		t.Run(name, func(t *testing.T) {
			state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
			require.NoError(t, err)
			first := &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{
				{Index: 4, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{geminiTestToolDelta(2, "call_parallel", "lookup", `{"city":"`), geminiTestToolDelta(0, "call_primary", "weather", `{"units":`)}}},
				{Index: 8, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{geminiTestToolDelta(0, "call_other_choice", "clock", `{"zone":"`)}}},
			}}
			first.Choices[0].Delta.SetContentString("Checking both tools.")
			results, err := ConvertStreamResponseChunk(context.Background(), info, state, first)
			require.NoError(t, err)
			assert.Empty(t, geminiTestCalls(t, results))
			require.Len(t, results, 1)
			assert.Equal(t, "Checking both tools.", results[0].Value.(*dto.GeminiChatResponse).Candidates[0].Content.Parts[0].Text)
			finish := "tool_calls"
			middle := &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{
				{Index: 8, FinishReason: &finish, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{geminiTestToolDelta(0, "", "", `UTC"}`)}}},
				{Index: 4, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{geminiTestToolDelta(0, "", "", `"metric"}`), geminiTestToolDelta(2, "", "", `Paris"}`)}}},
			}}
			results, err = ConvertStreamResponseChunk(context.Background(), info, state, middle)
			require.NoError(t, err)
			assert.Equal(t, map[int64][]dto.FunctionCall{8: {{ID: "call_other_choice", FunctionName: "clock", Arguments: map[string]any{"zone": "UTC"}}}}, geminiTestCalls(t, results))
			terminal := &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 4, FinishReason: &finish}}}
			results, err = ConvertStreamResponseChunk(context.Background(), info, state, terminal)
			require.NoError(t, err)
			assert.Equal(t, map[int64][]dto.FunctionCall{4: {
				{ID: "call_primary", FunctionName: "weather", Arguments: map[string]any{"units": "metric"}},
				{ID: "call_parallel", FunctionName: "lookup", Arguments: map[string]any{"city": "Paris"}},
			}}, geminiTestCalls(t, results))
			results, err = ConvertStreamResponseChunk(context.Background(), info, state, terminal)
			require.NoError(t, err)
			assert.Empty(t, geminiTestCalls(t, results), "a repeated terminal cannot emit duplicate function calls")
			results, err = FinalizeStreamResponse(context.Background(), info, state)
			require.NoError(t, err)
			assert.Empty(t, results)
		})
	}
}

func TestOpenAIToGeminiStreamsDoNotShareMetadataToolBuffers(t *testing.T) {
	info := &convmeta.Values{}
	first, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
	require.NoError(t, err)
	second, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
	require.NoError(t, err)
	for _, stream := range []struct {
		state *ResponseStreamState
		id    string
	}{{first, "call_first"}, {second, "call_second"}} {
		_, err := ConvertStreamResponseChunk(nil, info, stream.state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{geminiTestToolDelta(0, stream.id, "lookup", `{}`)}}}}})
		require.NoError(t, err)
	}
	finish := "tool_calls"
	for _, stream := range []struct {
		state *ResponseStreamState
		id    string
	}{{first, "call_first"}, {second, "call_second"}} {
		results, err := ConvertStreamResponseChunk(nil, info, stream.state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &finish}}})
		require.NoError(t, err)
		calls := geminiTestCalls(t, results)
		require.Len(t, calls[0], 1)
		assert.Equal(t, stream.id, calls[0][0].ID)
	}
}

func TestOpenAIToGeminiRejectsInvalidOrUnfinishedToolArguments(t *testing.T) {
	finish := "tool_calls"
	for _, arguments := range []string{`{"q":`, `[]`, `null`, `"text"`} {
		t.Run(arguments, func(t *testing.T) {
			state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
			require.NoError(t, err)
			chunk := &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 3, FinishReason: &finish, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{geminiTestToolDelta(2, "call_invalid", "lookup", arguments)}}}}}
			results, err := ConvertStreamResponseChunk(nil, nil, state, chunk)
			require.ErrorContains(t, err, "choice 3 index 2")
			assert.Empty(t, results)
			results, err = FinalizeStreamResponse(nil, nil, state)
			require.Error(t, err)
			assert.Empty(t, results)
			legacy, err := ConvertStreamResponse(nil, nil, types.RelayFormatGemini, chunk)
			require.Error(t, err, "stateless entry must surface the same invalid JSON")
			assert.Nil(t, legacy)
		})
	}
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(nil, nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{geminiTestToolDelta(0, "call_unfinished", "lookup", `{}`)}}}}})
	require.NoError(t, err)
	results, err := FinalizeStreamResponse(nil, nil, state)
	require.ErrorContains(t, err, "incomplete streamed tool call")
	assert.Empty(t, results, "finalization cannot invent a successful tool terminal")
}

func TestGeminiToOpenAIPreservesExplicitIDsAndChoiceTerminals(t *testing.T) {
	stop := "STOP"
	tool := func(id string) dto.GeminiPart {
		return dto.GeminiPart{FunctionCall: &dto.FunctionCall{ID: id, FunctionName: "lookup", Arguments: map[string]any{"q": id}}}
	}
	full := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{
		{Index: 3, FinishReason: &stop, Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{tool("provider_a"), tool("provider_b")}}},
		{Index: 9, FinishReason: &stop, Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "plain text"}}}},
	}}
	response, err := ConvertResponse(nil, nil, types.RelayFormatOpenAI, full)
	require.NoError(t, err)
	chat := response.Value.(*dto.OpenAITextResponse)
	assert.Equal(t, "tool_calls", chat.Choices[0].FinishReason)
	assert.Equal(t, "stop", chat.Choices[1].FinishReason)
	assert.Equal(t, "provider_a", chat.Choices[0].Message.ParseToolCalls()[0].ID)
	assert.Equal(t, "provider_b", chat.Choices[0].Message.ParseToolCalls()[1].ID)
	state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAI, ResponseStreamOptions{ID: "chat-fixed", Created: 1})
	require.NoError(t, err)
	full.Candidates[0].FinishReason = nil
	results, err := ConvertStreamResponseChunk(nil, nil, state, full)
	require.NoError(t, err)
	require.Len(t, results, 2)
	first := results[0].Value.(*dto.ChatCompletionsStreamResponse)
	assert.Equal(t, "provider_a", first.Choices[0].Delta.ToolCalls[0].ID)
	assert.Equal(t, 0, *first.Choices[0].Delta.ToolCalls[0].Index)
	assert.Equal(t, "provider_b", first.Choices[0].Delta.ToolCalls[1].ID)
	assert.Equal(t, 1, *first.Choices[0].Delta.ToolCalls[1].Index)
	assert.Nil(t, first.Choices[0].FinishReason)
	terminal := results[1].Value.(*dto.ChatCompletionsStreamResponse)
	assert.Equal(t, 9, terminal.Choices[0].Index)
	assert.Equal(t, "stop", *terminal.Choices[0].FinishReason)
	results, err = ConvertStreamResponseChunk(nil, nil, state, &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Index: 3, FinishReason: &stop, Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{tool("provider_c")}}}}})
	require.NoError(t, err)
	require.Len(t, results, 2)
	last := results[0].Value.(*dto.ChatCompletionsStreamResponse)
	assert.Equal(t, "provider_c", last.Choices[0].Delta.ToolCalls[0].ID)
	assert.Equal(t, 2, *last.Choices[0].Delta.ToolCalls[0].Index)
	terminal = results[1].Value.(*dto.ChatCompletionsStreamResponse)
	assert.Equal(t, 3, terminal.Choices[0].Index)
	assert.Equal(t, "tool_calls", *terminal.Choices[0].FinishReason)
	results, err = FinalizeStreamResponse(nil, nil, state)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestGeminiToolCallsPreserveNonStopFinishReasons(t *testing.T) {
	for _, test := range []struct{ finish, expected string }{{"MAX_TOKENS", "length"}, {"SAFETY", "content_filter"}} {
		t.Run(test.finish, func(t *testing.T) {
			chunk := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{Index: 5, FinishReason: &test.finish, Content: dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{FunctionCall: &dto.FunctionCall{ID: "provider_partial", FunctionName: "lookup", Arguments: map[string]any{}}}}}}}}
			full, err := ConvertResponse(nil, nil, types.RelayFormatOpenAI, chunk)
			require.NoError(t, err)
			assert.Equal(t, test.expected, full.Value.(*dto.OpenAITextResponse).Choices[0].FinishReason)
			state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAI, ResponseStreamOptions{})
			require.NoError(t, err)
			results, err := ConvertStreamResponseChunk(nil, nil, state, chunk)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, test.expected, *results[0].Value.(*dto.ChatCompletionsStreamResponse).Choices[0].FinishReason)
			results, err = FinalizeStreamResponse(nil, nil, state)
			require.NoError(t, err)
			assert.Empty(t, results)
		})
	}
}

func geminiTestToolDelta(index int, id, name, arguments string) dto.ToolCallResponse {
	return dto.ToolCallResponse{Index: &index, ID: id, Type: "function", Function: dto.FunctionResponse{Name: name, Arguments: arguments}}
}

func geminiTestCalls(t *testing.T, results []ResponseResult) map[int64][]dto.FunctionCall {
	t.Helper()
	calls := map[int64][]dto.FunctionCall{}
	for _, result := range results {
		response, ok := result.Value.(*dto.GeminiChatResponse)
		require.True(t, ok)
		for _, candidate := range response.Candidates {
			for _, part := range candidate.Content.Parts {
				if part.FunctionCall != nil {
					calls[candidate.Index] = append(calls[candidate.Index], *part.FunctionCall)
				}
			}
		}
	}
	return calls
}
