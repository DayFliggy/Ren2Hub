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

func TestToolArgumentsAreNotProviderContinuationState(t *testing.T) {
	var input dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"id":"msg_test","type":"message","stop_reason":"tool_use","content":[{"type":"tool_use","id":"call_1","name":"verify_document","input":{"signature":"customer-signature","content":{"type":"file_citation"}}}]}`), &input))
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAI, &input)
	require.NoError(t, err)
	raw, err := kitutil.Marshal(result.Value)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "customer-signature")
}

func TestGeminiGroundingSurvivesResponsePivot(t *testing.T) {
	var input dto.GeminiChatResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"你好 world"}]},"finishReason":"STOP","groundingMetadata":{"webSearchQueries":["test"],"groundingChunks":[{"web":{"uri":"https://example.com/source","title":"Source"}}],"groundingSupports":[{"segment":{"startIndex":0,"endIndex":6,"text":"你好"},"groundingChunkIndices":[0]}]}}]}`), &input))
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
		t.Run(string(target), func(t *testing.T) {
			result, err := ConvertResponse(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}}, target, &input)
			if target == types.RelayFormatClaude {
				require.ErrorContains(t, err, "native_state_required")
				return
			}
			require.NoError(t, err)
			raw, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			assert.Contains(t, string(raw), "https://example.com/source")
			assert.Contains(t, string(raw), "Source")
			if target != types.RelayFormatClaude {
				assert.Contains(t, string(raw), `"end_index":2`)
			}
			if target == types.RelayFormatOpenAIResponses {
				assert.Contains(t, string(raw), `"type":"web_search_call"`)
			}
		})
	}
}

func TestClaudeHostedResponseKeepsCallIdentityAndCitations(t *testing.T) {
	var input dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"id":"msg_test","type":"message","stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{"query":"test"}},{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[{"type":"web_search_result","url":"https://example.com","title":"Source"}]},{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"https://example.com","title":"Source","cited_text":"answer"}]}]}`), &input))
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, &input)
	require.NoError(t, err)
	response := result.Value.(*dto.OpenAIResponsesResponse)
	require.Len(t, response.Output, 2)
	assert.Equal(t, "web_search_call", response.Output[0].Type)
	assert.Equal(t, "srv_1", response.Output[0].ID)
	assert.Equal(t, "completed", response.Output[0].Status)
	assert.NotEmpty(t, response.Output[1].Content[0].Annotations)
	assert.NotEmpty(t, result.Diagnostics)
	_, err = ConvertResponse(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyStrict}}, types.RelayFormatOpenAIResponses, &input)
	require.Error(t, err)
}

func TestClaudeHostedStreamPreservesOneCallAcrossFinalSnapshots(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_test"})
	require.NoError(t, err)
	info := &convmeta.Values{}
	frames := []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"test","usage":{"input_tokens":3,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"server_tool_use","id":"srv_1","name":"web_search","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"test\"}"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv_1","content":[]}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":"answer"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.com","title":"Source","cited_text":"answer"}}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	}
	var results []ResponseResult
	for _, frame := range frames {
		var chunk dto.ClaudeResponse
		require.NoError(t, kitutil.Unmarshal([]byte(frame), &chunk))
		out, err := ConvertStreamResponseChunk(context.Background(), info, state, &chunk)
		require.NoError(t, err)
		results = append(results, out...)
	}
	final, err := FinalizeStreamResponseWithOutcome(context.Background(), info, state, StreamOutcomeCompleted)
	require.NoError(t, err)
	results = append(results, final...)
	var added, done int
	var completed *dto.OpenAIResponsesResponse
	for _, result := range results {
		event, ok := result.Value.(ChatToResponsesStreamEvent)
		if !ok {
			continue
		}
		if event.Payload.Item != nil && event.Payload.Item.ID == "srv_1" {
			if event.Type == "response.output_item.added" {
				added++
			}
			if event.Type == "response.output_item.done" {
				done++
			}
		}
		if event.Type == "response.completed" {
			completed = event.Payload.Response
		}
	}
	assert.Equal(t, 1, added)
	assert.Equal(t, 1, done)
	require.NotNil(t, completed)
	require.Len(t, completed.Output, 2)
	assert.Equal(t, "web_search_call", completed.Output[0].Type)
	require.Len(t, completed.Output[1].Content[0].Annotations, 1)
}

func TestResponseAnnotationEventsAreNotLostOrDuplicatedAtTerminal(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	frames := []string{
		`{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_1","delta":"hello"}`,
		`{"type":"response.output_text.annotation.added","output_index":0,"content_index":0,"item_id":"msg_1","annotation_index":0,"annotation":{"type":"url_citation","url":"https://example.com","title":"Source","start_index":0,"end_index":5}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg_1","content":[{"type":"output_text","text":"hello","annotations":[{"type":"url_citation","url":"https://example.com","title":"Source","start_index":0,"end_index":5}]}]}}`,
		`{"type":"response.completed","response":{"status":"completed","output":[]}}`,
	}
	var citations []any
	for _, frame := range frames {
		var input dto.ResponsesStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(frame), &input))
		results, err := ConvertStreamResponseChunk(context.Background(), nil, state, &input)
		require.NoError(t, err)
		for _, result := range results {
			chunk := result.Value.(dto.ChatCompletionsStreamResponse)
			for _, choice := range chunk.Choices {
				if len(choice.Delta.Annotations) > 0 {
					var values []any
					require.NoError(t, kitutil.Unmarshal(choice.Delta.Annotations, &values))
					citations = append(citations, values...)
				}
			}
		}
	}
	require.Len(t, citations, 1)
}

func TestLateGeminiGroundingStreamHasOneCitation(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatGemini, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_grounding"})
	require.NoError(t, err)
	frames := []string{
		`{"candidates":[{"index":0,"content":{"parts":[{"text":"你好"}]}}]}`,
		`{"candidates":[{"index":0,"content":{"parts":[]},"groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://example.com","title":"Source"}}],"groundingSupports":[{"segment":{"partIndex":0,"startIndex":0,"endIndex":6,"text":"你好"},"groundingChunkIndices":[0]}]}}]}`,
		`{"candidates":[{"index":0,"content":{"parts":[]},"finishReason":"STOP","groundingMetadata":{"groundingChunks":[{"web":{"uri":"https://example.com","title":"Source"}}],"groundingSupports":[{"segment":{"partIndex":0,"startIndex":0,"endIndex":6,"text":"你好"},"groundingChunkIndices":[0]}]}}]}`,
	}
	var results []ResponseResult
	for _, frame := range frames {
		var input dto.GeminiChatResponse
		require.NoError(t, kitutil.Unmarshal([]byte(frame), &input))
		out, err := ConvertStreamResponseChunk(context.Background(), nil, state, &input)
		require.NoError(t, err)
		results = append(results, out...)
	}
	final, err := FinalizeStreamResponseWithOutcome(context.Background(), nil, state, StreamOutcomeCompleted)
	require.NoError(t, err)
	results = append(results, final...)
	annotations := 0
	for _, result := range results {
		event := result.Value.(ChatToResponsesStreamEvent)
		if event.Type == "response.output_text.annotation.added" {
			annotations++
		}
		if event.Type == "response.completed" {
			require.Len(t, event.Payload.Response.Output[0].Content[0].Annotations, 1)
		}
	}
	assert.Equal(t, 1, annotations)
}

func TestUnknownHostedResultIsRejectedEvenUnderAllow(t *testing.T) {
	input := &dto.ClaudeResponse{StopReason: "end_turn", Content: []dto.ClaudeMediaMessage{{Type: "server_tool_use", Id: "srv", Name: "unknown_new_tool"}}}
	_, err := ConvertResponse(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}}, types.RelayFormatOpenAIResponses, input)
	require.Error(t, err)
}

func TestNativeClaudeCitationsRetainOriginalSourceState(t *testing.T) {
	var input dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"prefix "},{"type":"text","text":"cited","citations":[{"type":"web_search_result_location","url":"https://example.test","title":"source","cited_text":"original quotation","encrypted_index":"provider-state"}]},{"type":"text","text":" suffix"}]}`), &input))
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatClaude, &input)
	require.NoError(t, err)
	assert.Same(t, &input, result.Value)
}

func TestLatePartialCitationCannotPretendToCoverWholeClaudeStream(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatClaude, ResponseStreamOptions{})
	require.NoError(t, err)
	text := "prefix cited suffix"
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}}}})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Annotations: []byte(`[{"type":"url_citation","url_citation":{"url":"https://example.com","start_index":7,"end_index":12}}]`)}}}})
	require.ErrorContains(t, err, "native_state_required")
	_, err = FinalizeStreamResponseWithOutcome(context.Background(), nil, state, StreamOutcomeCompleted)
	require.Error(t, err)
}

func TestHostedToolKeepsTextOnBothSidesInStream(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_order"})
	require.NoError(t, err)
	frames := []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"before"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"test"}}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"web_search_tool_result","tool_use_id":"srv","content":[]}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"text","text":"after"}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"citations_delta","citation":{"type":"web_search_result_location","url":"https://example.com","cited_text":"source quotation"}}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"}}`,
	}
	for _, frame := range frames {
		var input dto.ClaudeResponse
		require.NoError(t, kitutil.Unmarshal([]byte(frame), &input))
		_, err := ConvertStreamResponseChunk(context.Background(), nil, state, &input)
		require.NoError(t, err)
	}
	results, err := FinalizeStreamResponseWithOutcome(context.Background(), nil, state, StreamOutcomeCompleted)
	require.NoError(t, err)
	var response *dto.OpenAIResponsesResponse
	for _, result := range results {
		event := result.Value.(ChatToResponsesStreamEvent)
		if event.Type == "response.completed" {
			response = event.Payload.Response
		}
	}
	require.NotNil(t, response)
	require.Len(t, response.Output, 3)
	assert.Equal(t, "before", response.Output[0].Content[0].Text)
	assert.Equal(t, "web_search_call", response.Output[1].Type)
	assert.Equal(t, "after", response.Output[2].Content[0].Text)
	annotation := response.Output[2].Content[0].Annotations[0].(map[string]any)
	assert.Equal(t, "source quotation", annotation["cited_text"])
	assert.NotContains(t, annotation, "start_index", "source quotation must not fabricate answer offsets")
	assert.Equal(t, "beforeafter", state.UsageText())
}

func TestHostedToolKeepsNonStreamTextOrder(t *testing.T) {
	var input dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"before"},{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"test"}},{"type":"web_search_tool_result","tool_use_id":"srv","content":[]},{"type":"text","text":"after"}]}`), &input))
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, &input)
	require.NoError(t, err)
	response := result.Value.(*dto.OpenAIResponsesResponse)
	require.Len(t, response.Output, 3)
	assert.Equal(t, "before", response.Output[0].Content[0].Text)
	assert.Equal(t, "web_search_call", response.Output[1].Type)
	assert.Equal(t, "after", response.Output[2].Content[0].Text)
}
