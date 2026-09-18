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

func TestResponsePauseNeverBecomesCompleted(t *testing.T) {
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow} {
		t.Run(string(policy), func(t *testing.T) {
			info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}}
			input := &dto.ClaudeResponse{StopReason: "pause_turn"}
			result, err := ConvertResponse(context.Background(), info, types.RelayFormatOpenAIResponses, input)
			require.Error(t, err)
			assert.Nil(t, result)
			_, err = ConvertResponse(context.Background(), info, types.RelayFormatClaude, input)
			require.NoError(t, err)
		})
	}
}

func TestUnknownResponseIncompleteCannotBecomeLength(t *testing.T) {
	input := &dto.OpenAIResponsesResponse{Status: []byte(`"incomplete"`), IncompleteDetails: &dto.IncompleteDetails{Reason: "new_provider_reason"}}
	_, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAI, input)
	require.Error(t, err)
}

func TestLimitedResponseRetainsBodyAndUsage(t *testing.T) {
	input := &dto.OpenAIResponsesResponse{Status: []byte(`"incomplete"`), IncompleteDetails: &dto.IncompleteDetails{Reason: "max_output_tokens"}, Output: []dto.ResponsesOutput{{Type: "message", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "partial"}}}}, Usage: &dto.Usage{InputTokens: 4, OutputTokens: 2, TotalTokens: 6}}
	result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAI, input)
	require.NoError(t, err)
	chat := result.Value.(*dto.OpenAITextResponse)
	assert.Equal(t, "partial", chat.Choices[0].Message.StringContent())
	assert.Equal(t, "length", chat.Choices[0].FinishReason)
	assert.Equal(t, 6, result.Usage.TotalTokens)
}

func TestPausedStreamRetainsUsageAndCannotBeFinalizedAsSuccess(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, ResponseStreamOptions{})
	require.NoError(t, err)
	reason := "pause_turn"
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &dto.ClaudeResponse{Type: "message_delta", Delta: &dto.ClaudeMediaMessage{StopReason: &reason}, Usage: &dto.ClaudeUsage{OutputTokens: 9}})
	require.Error(t, err)
	assert.Equal(t, types.ResponseProviderPause, state.Termination().Kind)
	require.NotNil(t, state.Usage())
	assert.Equal(t, 9, state.Usage().CompletionTokens)
	results, err := FinalizeStreamResponseWithOutcome(context.Background(), nil, state, StreamOutcomeCompleted)
	require.Error(t, err)
	assert.Empty(t, results)
}
