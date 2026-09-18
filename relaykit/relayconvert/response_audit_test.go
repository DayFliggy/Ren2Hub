package relayconvert

import (
	"context"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseAuditRejectsUnknownSecondCandidateTermination(t *testing.T) {
	response := &dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{Index: 0, FinishReason: "stop"}, {Index: 1, FinishReason: "provider_new_reason"}}}
	assert.Equal(t, types.ResponseUnknown, TerminationFromResponse(response).Kind)
	_, err := ConvertResponse(context.Background(), nil, types.RelayFormatGemini, response)
	require.Error(t, err)
}

func TestResponseAuditNonStreamMissingTerminalCannotBecomeCompleted(t *testing.T) {
	for _, response := range []any{
		&dto.OpenAITextResponse{},
		&dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{Message: dto.Message{Content: "partial"}}}},
		&dto.OpenAIResponsesResponse{Status: []byte(`"in_progress"`)},
		&dto.ClaudeResponse{Type: "message"},
	} {
		target := types.RelayFormat(types.RelayFormatGemini)
		_, err := ConvertResponse(context.Background(), nil, target, response)
		require.ErrorContains(t, err, "generation terminal")
	}
}

func TestResponseAuditNeverMergesMultipleChoicesIntoResponses(t *testing.T) {
	response := &dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{Index: 0, Message: dto.Message{Content: "first"}, FinishReason: "stop"}, {Index: 1, Message: dto.Message{Content: "second"}, FinishReason: "stop"}}}
	_, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, response)
	require.Error(t, err)
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{})
	require.NoError(t, err)
	text := "second"
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 1, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}}}})
	require.Error(t, err)
}

func TestResponseAuditAllStreamChoicesMustTerminate(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
	require.NoError(t, err)
	text, stop := "partial", "stop"
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &stop}, {Index: 1, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}}}})
	require.NoError(t, err)
	_, err = FinalizeStreamResponseWithOutcome(context.Background(), nil, state, StreamOutcomeCompleted)
	require.ErrorIs(t, err, ErrStreamTerminalMissing)
}

func TestResponseAuditMissingTerminalCannotBecomeSuccess(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{})
	require.NoError(t, err)
	text := "partial"
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}}}})
	require.NoError(t, err)
	_, err = FinalizeStreamResponse(context.Background(), nil, state)
	require.Error(t, err)
}

func TestResponseAuditGeminiGroundingOutOfRangeIsExplicitFailure(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	var response dto.GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"text":"abc"}]},"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://example.com"}}],"groundingSupports":[{"segment":{"startIndex":0,"endIndex":999},"groundingChunkIndices":[0]}]}}]}`), &response))
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &response)
	require.Error(t, err)
}

func TestResponseAuditStrictRejectsDroppedSearchPresentation(t *testing.T) {
	var response dto.GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"text":"answer"}]},"finishReason":"STOP","groundingMetadata":{"searchEntryPoint":{"renderedContent":"<div>sources</div>"}}}]}`), &response))
	_, err := ConvertResponse(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyStrict}}, types.RelayFormatOpenAI, &response)
	require.Error(t, err)
}

func TestResponseAuditHostedSearchFailureCannotBecomeCompleted(t *testing.T) {
	var response dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"test"}},{"type":"web_search_tool_result","tool_use_id":"srv","content":{"type":"web_search_tool_result_error","error_code":"max_uses_exceeded"}}]}`), &response))
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, &response)
	require.NoError(t, err)
	assert.Equal(t, "failed", result.Value.(*dto.OpenAIResponsesResponse).Output[0].Status)
}

func TestResponseAuditConversionStreamHasCumulativeMemoryBound(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{MaxInputBytes: 4096})
	require.NoError(t, err)
	text := strings.Repeat("x", 2100)
	frame := &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}}}}
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, frame)
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, frame)
	require.ErrorContains(t, err, "response_conversion_limit")
	_, err = FinalizeStreamResponseWithOutcome(context.Background(), nil, state, StreamOutcomeCompleted)
	require.ErrorContains(t, err, "response_conversion_limit")
}

func TestResponseAuditRepeatedTerminalNeverEmitsAfterFinish(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	frame := &dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{{Type: "message", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "answer"}}}}}}
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, frame)
	require.NoError(t, err)
	results, err := ConvertStreamResponseChunk(context.Background(), nil, state, frame)
	require.NoError(t, err)
	assert.Empty(t, results)
	frame.Response.Output[0].Content[0].Text = "changed"
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, frame)
	require.ErrorContains(t, err, "response_terminal_conflict")
}

func TestResponseAuditConflictingFailureEventNeverBecomesSuccess(t *testing.T) {
	frame := &dto.ResponsesStreamResponse{Type: "response.failed", Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`)}}
	assert.Equal(t, types.ResponseFailed, TerminationFromResponse(frame).Kind)
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, frame)
	require.Error(t, err)
}

func TestResponseAuditTerminalSnapshotCannotSilentlyDropNativeCitation(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatClaude, ResponseStreamOptions{})
	require.NoError(t, err)
	var frame dto.ResponsesStreamResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"answer","annotations":[{"type":"file_citation","file_id":"file_opaque","index":0}]}]}]}}`), &frame))
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &frame)
	require.ErrorContains(t, err, "citation_source_unrepresentable")
}

func TestResponseAuditHostedSearchCannotChangeActionOrHideFailure(t *testing.T) {
	for _, item := range []dto.ResponsesOutput{
		{Type: "web_search_call", ID: "ws", Status: "completed", Action: []byte(`{"type":"open_page","url":"https://example.com"}`)},
		{Type: "web_search_call", ID: "ws", Status: "failed", Action: []byte(`{"type":"search","query":"test"}`)},
	} {
		_, err := ConvertResponse(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}}, types.RelayFormatClaude, &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{item}})
		require.Error(t, err)
	}
}
