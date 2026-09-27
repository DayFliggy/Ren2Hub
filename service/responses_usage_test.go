package service

import (
	"github.com/QuantumNous/new-api/common"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesUsagePresenceSurvivesConversionAndTerminal(t *testing.T) {
	for _, test := range []struct {
		raw      string
		complete bool
	}{
		{`{}`, false},
		{`{"input_tokens":12}`, false},
		{`{"input_tokens":null,"output_tokens":0}`, false},
		{`{"input_tokens":0,"output_tokens":0}`, true},
	} {
		t.Run(test.raw, func(t *testing.T) {
			var event dto.ResponsesStreamResponse
			require.NoError(t, common.Unmarshal([]byte(`{"type":"response.completed","response":{"usage":`+test.raw+`}}`), &event))
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}
			accounting := NewResponsesUsageAccumulator(info)
			accounting.Observe(&event)
			facts := NormalizedBillingUsage(accounting.Finish(), "openai")
			require.Equal(t, test.complete, facts.Complete)
		})
	}
}

func TestResponsesUsageFinishIsIdempotent(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"}}
	accounting := NewResponsesUsageAccumulator(info)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_item.done", Item: &dto.ResponsesOutput{Type: dto.ResponsesOutputTypeImageGenerationCall, ID: "image_1", Status: "completed", Result: "image"}})
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "The generated image is ready."})
	usage := accounting.Finish()
	require.Positive(t, usage.CompletionTokens)
	assert.Equal(t, "estimated_partial", usage.UsageSource)
	require.NotNil(t, usage.BillingUsage)
	assert.True(t, usage.BillingUsage.Estimated)
	assert.Equal(t, usage.CompletionTokens, effectiveBillingUsage(usage).CompletionTokens)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.failed"})
	assert.Same(t, usage, accounting.Finish())
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
}

func TestResponsesUsageEstimateRefreshesNativeBillingSnapshot(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"}}
	accounting := NewResponsesUsageAccumulator(info)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.created", Response: &dto.OpenAIResponsesResponse{Usage: &dto.Usage{InputTokens: 100, InputTokensDetails: &dto.InputTokenDetails{CachedTokens: 10, AudioTokens: 2}}}})
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "An interrupted response with a real output."})
	usage := accounting.Finish()
	require.Positive(t, usage.CompletionTokens)
	require.NotNil(t, usage.BillingUsage)
	assert.True(t, usage.BillingUsage.Estimated)
	billed := effectiveBillingUsage(usage)
	assert.Equal(t, usage.CompletionTokens, billed.CompletionTokens)
	assert.Equal(t, 100, billed.PromptTokens)
	assert.Equal(t, 10, billed.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 2, billed.PromptTokensDetails.AudioTokens)
}

func TestResponsesUsagePreservesAuthoritativeForeignBillingSnapshot(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"}}
	accounting := NewResponsesUsageAccumulator(info)
	authoritative := dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 10, OutputTokens: 12, CacheReadInputTokens: 20})
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.created", Response: &dto.OpenAIResponsesResponse{Usage: &dto.Usage{InputTokens: 30, BillingUsage: authoritative}}})
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "converted output"})
	usage := accounting.Finish()
	assert.Equal(t, authoritative, usage.BillingUsage)
	assert.NotSame(t, authoritative, usage.BillingUsage)
	assert.Equal(t, 12, effectiveBillingUsage(usage).CompletionTokens)
}
