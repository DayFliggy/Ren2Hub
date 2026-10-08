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
	"github.com/tidwall/gjson"
)

func TestChatOutputFieldsRemainNativeAndRejectLossyConversion(t *testing.T) {
	for _, field := range []string{`"refusal":"declined"`, `"audio":{"id":"audio_1","data":"YQ=="}`} {
		var response dto.OpenAITextResponse
		require.NoError(t, kitutil.Unmarshal([]byte(`{"choices":[{"index":0,"message":{"role":"assistant",`+field+`},"finish_reason":"stop"}]}`), &response))
		native, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAI, &response)
		require.NoError(t, err)
		encoded, err := kitutil.Marshal(native.Value)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), field)
		for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
			_, err := ConvertResponse(context.Background(), nil, target, &response)
			require.ErrorContains(t, err, "choices[0].message")
		}
	}
}

func TestResponsesRefusalReachesClaudeUnderConversionPolicy(t *testing.T) {
	refusal := "I can't help with that."
	messageItem := dto.ResponsesOutput{Type: "message", ID: "msg_1", Role: "assistant", Content: []dto.ResponsesOutputContent{{Type: "refusal", Refusal: &refusal}}}
	response := &dto.OpenAIResponsesResponse{ID: "resp_1", Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{messageItem}}
	events := []*dto.ResponsesStreamResponse{
		{Type: "response.output_item.added", OutputIndex: kitutil.GetPointer(0), ItemID: "msg_1", Item: &dto.ResponsesOutput{Type: "message", ID: "msg_1", Role: "assistant"}},
		{Type: "response.refusal.delta", OutputIndex: kitutil.GetPointer(0), ItemID: "msg_1", Delta: refusal},
		{Type: "response.refusal.done", OutputIndex: kitutil.GetPointer(0), ItemID: "msg_1", Refusal: &refusal},
		{Type: "response.output_item.done", OutputIndex: kitutil.GetPointer(0), ItemID: "msg_1", Item: &messageItem},
		{Type: "response.completed", Response: response},
	}

	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow, types.ConversionLossPolicyStrict} {
		t.Run(string(policy), func(t *testing.T) {
			info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}}
			result, err := ConvertResponse(context.Background(), info, types.RelayFormatClaude, response)
			if policy == types.ConversionLossPolicyStrict {
				require.ErrorContains(t, err, "output[0].content[0].refusal")
			} else {
				require.NoError(t, err)
				claude, ok := result.Value.(*dto.ClaudeResponse)
				require.True(t, ok)
				assert.Equal(t, "refusal", claude.StopReason)
				require.Len(t, claude.Content, 1)
				require.NotNil(t, claude.Content[0].Text)
				assert.Equal(t, refusal, *claude.Content[0].Text)
				require.Len(t, result.Diagnostics, 1)
				assert.Equal(t, "responses_refusal_reduced", result.Diagnostics[0].Code)
				assert.Equal(t, "output[0].content[0].refusal", result.Diagnostics[0].Path)
				assert.Equal(t, types.ConversionDiagnosticWarning, result.Diagnostics[0].Severity)
			}

			state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatClaude, ResponseStreamOptions{ID: "resp_1", Model: "gpt-test"})
			require.NoError(t, err)
			var text strings.Builder
			var streamErr error
			for _, event := range events {
				var results []ResponseResult
				results, streamErr = ConvertStreamResponseChunk(context.Background(), info, state, event)
				if streamErr != nil {
					break
				}
				for _, result := range results {
					chunk, ok := result.Value.(*dto.ClaudeResponse)
					if ok && chunk.Type == "content_block_delta" && chunk.Delta != nil && chunk.Delta.Text != nil {
						text.WriteString(*chunk.Delta.Text)
					}
				}
			}
			if policy == types.ConversionLossPolicyStrict {
				require.ErrorContains(t, streamErr, "refusal")
				assert.Empty(t, text.String())
				return
			}
			require.NoError(t, streamErr)
			assert.Equal(t, refusal, text.String(), "the refusal must be delivered exactly once")
			diagnostics := state.Diagnostics()
			require.Len(t, diagnostics, 1)
			assert.Equal(t, "responses_refusal_reduced", diagnostics[0].Code)
			assert.Equal(t, "refusal", diagnostics[0].Path)
			assert.Equal(t, types.ConversionDiagnosticWarning, diagnostics[0].Severity)
		})
	}

	t.Run("chat target keeps the refusal natively under strict", func(t *testing.T) {
		info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyStrict}}
		_, err := ConvertResponse(context.Background(), info, types.RelayFormatOpenAI, response)
		require.NoError(t, err)
		state, err := NewResponseStreamState(types.RelayFormatOpenAIResponses, types.RelayFormatOpenAI, ResponseStreamOptions{ID: "resp_1", Model: "gpt-test"})
		require.NoError(t, err)
		for _, event := range events {
			_, err := ConvertStreamResponseChunk(context.Background(), info, state, event)
			require.NoError(t, err)
		}
	})
}

func TestChatStreamDTOAndNativeRoutePreserveOutputFields(t *testing.T) {
	body := []byte(`{"choices":[{"index":0,"delta":{"refusal":"declined","audio":{"data":"YQ==","transcript":"voice"},"reasoning_content":"thought"},"logprobs":{"content":[{"token":"word","logprob":-0.1}]}}]}`)
	var response dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal(body, &response))
	result, err := ConvertStreamResponse(context.Background(), nil, types.RelayFormatOpenAI, &response)
	require.NoError(t, err)
	encoded, err := kitutil.Marshal(result.Value)
	require.NoError(t, err)
	for _, path := range []string{"choices.0.delta.refusal", "choices.0.delta.audio", "choices.0.delta.reasoning_content", "choices.0.logprobs"} {
		assert.JSONEq(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(encoded, path).Raw)
	}
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &response)
	require.ErrorContains(t, err, "choices[0].delta")
}
