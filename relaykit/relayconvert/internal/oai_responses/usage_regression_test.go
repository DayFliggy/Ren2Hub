package oairesponses

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesConversionsPreserveNativeUsageDetails(t *testing.T) {
	var response dto.OpenAIResponsesResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"id":"resp_1","status":"completed","model":"gpt-test","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}],"usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":10,"cache_write_tokens":3,"audio_tokens":2,"image_tokens":8,"text_tokens":90},"output_tokens_details":{"reasoning_tokens":6,"audio_tokens":4,"image_tokens":5,"text_tokens":11}}}`), &response))
	chat, usage, err := ResponsesResponseToChatCompletionsResponse(&response, "chat_1")
	require.NoError(t, err)
	assert.Equal(t, "answer", chat.Choices[0].Message.StringContent())
	state := NewResponsesToChatStreamState("gpt-test", true)
	chunks, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{Type: "response.completed", Response: &response}, state)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	for _, actual := range []*dto.Usage{usage, &chat.Usage, state.Usage} {
		require.NotNil(t, actual)
		assert.Equal(t, *response.Usage.InputTokensDetails, actual.PromptTokensDetails)
		assert.Equal(t, *response.Usage.OutputTokensDetails, actual.CompletionTokenDetails)
		require.NotNil(t, actual.BillingUsage)
		require.NotNil(t, actual.BillingUsage.OpenAIUsage)
		assert.Equal(t, actual.PromptTokensDetails, actual.BillingUsage.OpenAIUsage.PromptTokensDetails)
		assert.Equal(t, actual.CompletionTokenDetails, actual.BillingUsage.OpenAIUsage.CompletionTokenDetails)
		assert.Equal(t, 120, actual.TotalTokens)
	}
	response.Usage.InputTokensDetails.AudioTokens = 999
	response.Usage.OutputTokensDetails.AudioTokens = 999
	assert.Equal(t, 2, usage.InputTokensDetails.AudioTokens)
	assert.Equal(t, 4, usage.OutputTokensDetails.AudioTokens)
	assert.Equal(t, 4, usage.BillingUsage.OpenAIUsage.OutputTokensDetails.AudioTokens)
}

func TestResponsesUsagePrefersNativeOutputDetailsIncludingZero(t *testing.T) {
	usage := UsageFromResponsesUsage(&dto.Usage{InputTokens: 5, OutputTokens: 3, CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 2}, OutputTokensDetails: &dto.OutputTokenDetails{}})
	assert.Zero(t, usage.CompletionTokenDetails.AudioTokens)
	require.NotNil(t, usage.BillingUsage)
	assert.Zero(t, usage.BillingUsage.OpenAIUsage.CompletionTokenDetails.AudioTokens)
}
