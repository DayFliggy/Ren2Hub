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

func TestConversionPlanPoliciesAndNativeState(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`"hi"`), Tools: []byte(`[{"type":"code_interpreter","container":{"type":"auto"}}]`)}
	features, err := AnalyzeRequest(req)
	require.NoError(t, err)
	plan, err := PlanConversion(features, types.RelayFormatGemini, "")
	require.NoError(t, err)
	assert.Equal(t, types.ConversionLossPolicySafe, plan.Policy)
	assert.NotEmpty(t, plan.Diagnostics)
	plan, err = PlanConversion(features, types.RelayFormatGemini, types.ConversionLossPolicyAllow)
	require.NoError(t, err)
	assert.NotEmpty(t, plan.RequestConverter)
	assert.NotEmpty(t, plan.ResponseConverter)
	req.PreviousResponseID = "resp_private"
	features, err = AnalyzeRequest(req)
	require.NoError(t, err)
	_, err = PlanConversion(features, types.RelayFormatGemini, types.ConversionLossPolicyAllow)
	require.Error(t, err)
	plan, err = PlanConversion(features, types.RelayFormatOpenAIResponses, types.ConversionLossPolicyStrict)
	require.NoError(t, err)
	assert.True(t, plan.Native)
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, req)
	require.NoError(t, err)
	assert.Same(t, req, result.Value)
}

func TestPortableSearchAndExplicitZeroAcrossProtocols(t *testing.T) {
	zero := float64(0)
	request := &dto.OpenAIResponsesRequest{Model: "gemini-test", Input: []byte(`"search"`), Tools: []byte(`[{"type":"web_search"}]`), TopP: &zero}
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, request)
	require.NoError(t, err)
	converted := result.Value.(*dto.GeminiChatRequest)
	require.Len(t, converted.GetTools(), 1)
	assert.NotNil(t, converted.GetTools()[0].GoogleSearch)
	require.NotNil(t, converted.GenerationConfig.TopP)
	assert.Zero(t, *converted.GenerationConfig.TopP)
	assert.JSONEq(t, `[{"type":"web_search"}]`, string(request.Tools))
	meta := &convmeta.Values{UpstreamModelName: "gpt-test", ChannelMetaAttached: true}
	result, err = ConvertRequest(context.Background(), meta, types.RelayFormatOpenAI, converted)
	require.NoError(t, err)
	chat := result.Value.(*dto.GeneralOpenAIRequest)
	require.NotNil(t, chat.TopP)
	assert.Zero(t, *chat.TopP)
	assert.NotNil(t, chat.WebSearchOptions)
}

func TestHostedSearchUsesUpstreamBridge(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`"search"`), Tools: []byte(`[{"type":"web_search"}]`)}
	requirements, err := AnalyzeRequest(request)
	require.NoError(t, err)
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		_, err := PlanConversion(requirements, types.RelayFormatClaude, policy)
		require.NoError(t, err)
	}
	_, err = PlanConversion(requirements, types.RelayFormatOpenAIResponses, types.ConversionLossPolicyStrict)
	require.NoError(t, err)
}

func TestExecuteConversionPlanKeepsPolicyAndRejectsChangedState(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`"hi"`), Tools: []byte(`[{"type":"custom","name":"shell"}]`)}
	requirements, err := AnalyzeRequest(req)
	require.NoError(t, err)
	plan, err := PlanConversion(requirements, types.RelayFormatGemini, types.ConversionLossPolicyAllow)
	require.NoError(t, err)
	result, err := ExecuteConversionPlan(context.Background(), nil, plan, req)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Diagnostics)
	assert.Equal(t, plan.RequestConverter, result.Converter)
	req.PreviousResponseID = "resp_private"
	_, err = ExecuteConversionPlan(context.Background(), nil, plan, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider-owned state")
	plan.RequestConverter = "invalid"
	req.PreviousResponseID = ""
	_, err = ExecuteConversionPlan(context.Background(), nil, plan, req)
	require.ErrorContains(t, err, "plan no longer matches")
}

func TestStrictPolicyAndToolSchemaSemantics(t *testing.T) {
	req := &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`"hi"`), Metadata: []byte(`{"tenant":"example"}`)}
	requirements, err := AnalyzeRequest(req)
	require.NoError(t, err)
	plan, err := PlanConversion(requirements, types.RelayFormatGemini, types.ConversionLossPolicySafe)
	require.NoError(t, err)
	assert.NotEmpty(t, plan.Diagnostics)
	_, err = PlanConversion(requirements, types.RelayFormatGemini, types.ConversionLossPolicyStrict)
	require.Error(t, err)
	_, err = PlanConversion(requirements, types.RelayFormatOpenAI, types.ConversionLossPolicyStrict)
	require.NoError(t, err)
	req.Tools = []byte(`[{"type":"function","name":"lookup","strict":false,"parameters":{"type":"object"}}]`)
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, req)
	require.NoError(t, err)
	chat := result.Value.(*dto.GeneralOpenAIRequest)
	require.Len(t, chat.Tools, 1)
	require.NotNil(t, chat.Tools[0].Function.Strict)
	assert.False(t, *chat.Tools[0].Function.Strict)
	req.Tools = []byte(`[{"type":"function","name":"lookup","parameters":{"type":"object","additionalProperties":false}}]`)
	_, err = ConvertRequest(context.Background(), nil, types.RelayFormatGemini, req)
	require.ErrorContains(t, err, "JSON Schema constraints")
}

func TestGeminiToolHistoryUsesExplicitIDsAndRejectsAmbiguity(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantError  bool
		wantIDs    []string
	}{
		{"ordinary", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, false, []string{"call_1"}},
		{"parallel explicit", `{"contents":[{"role":"model","parts":[{"functionCall":{"id":"a","name":"f","args":{}}},{"functionCall":{"id":"b","name":"f","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"id":"b","name":"f","response":{}}},{"functionResponse":{"id":"a","name":"f","response":{}}}]}]}`, false, []string{"b", "a"}},
		{"ambiguous", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"f","args":{}}},{"functionCall":{"name":"f","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, true, nil},
		{"unmatched", `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"f","response":{}}}]}]}`, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var req dto.GeminiChatRequest
			require.NoError(t, kitutil.Unmarshal([]byte(tc.body), &req))
			result, err := ConvertRequest(context.Background(), &convmeta.Values{UpstreamModelName: "gpt-test", ChannelMetaAttached: true}, types.RelayFormatOpenAI, &req)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			var ids []string
			for _, message := range result.Value.(*dto.GeneralOpenAIRequest).Messages {
				if message.Role == "tool" {
					ids = append(ids, message.ToolCallId)
				}
			}
			assert.Equal(t, tc.wantIDs, ids)
		})
	}
}

func TestResponsesStreamLifecycleAndOutcome(t *testing.T) {
	for _, outcome := range []StreamOutcome{StreamOutcomeCompleted, StreamOutcomeIncomplete, StreamOutcomeFailed, StreamOutcomeCancelled} {
		t.Run(string(outcome), func(t *testing.T) {
			state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_test", Model: "test", EmitSequenceNumber: true})
			require.NoError(t, err)
			text := "hello"
			chunk := &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text}}}}
			results, err := ConvertStreamResponseChunk(context.Background(), nil, state, chunk)
			require.NoError(t, err)
			require.Len(t, results, 3)
			assert.Equal(t, "response.output_text.delta", results[2].Value.(ChatToResponsesStreamEvent).Type)
			if outcome == StreamOutcomeCompleted {
				stop := "stop"
				terminal, err := ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{FinishReason: &stop}}})
				require.NoError(t, err)
				results = append(results, terminal...)
			}
			final, err := FinalizeStreamResponseWithOutcome(context.Background(), nil, state, outcome)
			require.NoError(t, err)
			if outcome == StreamOutcomeCancelled {
				assert.Empty(t, final)
			} else {
				require.NotEmpty(t, final)
				assert.Equal(t, "response."+string(outcome), final[len(final)-1].Value.(ChatToResponsesStreamEvent).Type)
			}
			for index, result := range append(results, final...) {
				event := result.Value.(ChatToResponsesStreamEvent)
				require.NotNil(t, event.Payload.SequenceNumber)
				assert.Equal(t, index, *event.Payload.SequenceNumber)
				if event.Type == "response.output_text.done" {
					require.NotNil(t, event.Payload.Text)
					assert.Equal(t, text, *event.Payload.Text)
				}
			}
			again, err := FinalizeStreamResponseWithOutcome(context.Background(), nil, state, outcome)
			require.NoError(t, err)
			assert.Empty(t, again)
			_, err = ConvertStreamResponseChunk(context.Background(), nil, state, chunk)
			require.Error(t, err)
		})
	}
}
