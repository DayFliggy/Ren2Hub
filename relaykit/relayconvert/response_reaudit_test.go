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

func TestClaudeCitationStateCannotBeInventedOrDropped(t *testing.T) {
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		meta := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}}
		chat := &dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{FinishReason: "stop", Message: dto.Message{Content: "answer", Annotations: []byte(`[{"type":"url_citation","url_citation":{"url":"https://example.test","start_index":0,"end_index":6}}]`)}}}}
		_, err := ConvertResponse(context.Background(), meta, types.RelayFormatClaude, chat)
		require.Error(t, err, "a generated answer is not a source quotation or an encrypted Claude citation")
		var claude dto.ClaudeResponse
		require.NoError(t, kitutil.Unmarshal([]byte(`{"stop_reason":"end_turn","content":[{"type":"text","text":"answer","citations":[{"type":"web_search_result_location","url":"https://example.test","cited_text":"source quote","encrypted_index":"opaque-provider-state"}]}]}`), &claude))
		_, err = ConvertResponse(context.Background(), meta, types.RelayFormatOpenAIResponses, &claude)
		require.Error(t, err, "provider-owned citation state must retain the original protocol")
	}
}

func TestClaudeSearchHasUpstreamConversionRoute(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"claude-test","max_tokens":16,"messages":[{"role":"user","content":"search"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`), &request))
	requirements, err := AnalyzeRequest(&request)
	require.NoError(t, err)
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		_, err := PlanConversion(requirements, target, types.ConversionLossPolicyAllow)
		require.NoError(t, err)
	}
	_, err = PlanConversion(requirements, types.RelayFormatClaude, types.ConversionLossPolicySafe)
	require.NoError(t, err)
}

func TestEncryptedHostedSearchResultsRequireNativeResponse(t *testing.T) {
	var response dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"stop_reason":"end_turn","content":[{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"test"}},{"type":"web_search_tool_result","tool_use_id":"srv","content":[{"type":"web_search_result","url":"https://example.test","title":"source","encrypted_content":"provider-state"}]}]}`), &response))
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		_, err := ConvertResponse(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}}, target, &response)
		require.Error(t, err)
	}
}

func TestResponsesStreamPartialSnapshotPreservesCitationIdentityAndOffsets(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	frames := []string{
		`{"type":"response.output_text.delta","output_index":0,"item_id":"first","delta":"aaa"}`,
		`{"type":"response.output_text.delta","output_index":1,"item_id":"second","delta":"bbb"}`,
		`{"type":"response.output_text.annotation.added","output_index":1,"item_id":"second","annotation":{"type":"url_citation","url":"https://second.test","start_index":0,"end_index":3}}`,
		`{"type":"response.completed","response":{"status":"completed","output":[{"id":"second","type":"message","content":[{"type":"output_text","text":"bbb"}]}]}}`,
	}
	var annotations []map[string]any
	for _, frame := range frames {
		var event dto.ResponsesStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(frame), &event))
		results, err := ConvertStreamResponseChunk(context.Background(), nil, state, &event)
		require.NoError(t, err)
		for _, result := range results {
			var chunk *dto.ChatCompletionsStreamResponse
			switch value := result.Value.(type) {
			case *dto.ChatCompletionsStreamResponse:
				chunk = value
			case dto.ChatCompletionsStreamResponse:
				chunk = &value
			default:
				continue
			}
			for _, choice := range chunk.Choices {
				if len(choice.Delta.Annotations) == 0 {
					continue
				}
				var found []map[string]any
				require.NoError(t, kitutil.Unmarshal(choice.Delta.Annotations, &found))
				annotations = append(annotations, found...)
			}
		}
	}
	require.Len(t, annotations, 1)
	citation := annotations[0]["url_citation"].(map[string]any)
	assert.Equal(t, "https://second.test", citation["url"])
	assert.Equal(t, float64(3), citation["start_index"])
	assert.Equal(t, float64(6), citation["end_index"])
}

func TestIncompleteEventCannotBorrowCompletedSnapshot(t *testing.T) {
	event := &dto.ResponsesStreamResponse{Type: "response.incomplete", Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), IncompleteDetails: &dto.IncompleteDetails{Reason: "future_reason"}}}
	assert.Equal(t, types.ResponseUnknown, TerminationFromResponse(event).Kind)
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, event)
	require.Error(t, err)
}

func TestResponsesCannotEmitTextAfterTerminal(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{Status: []byte(`"completed"`)}})
	require.NoError(t, err)
	results, err := ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "late text"})
	require.Error(t, err)
	assert.Empty(t, results)
}
