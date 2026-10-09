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
			accounting.Observe(&event, nil)
			facts := NormalizedBillingUsage(accounting.Finish(), "openai")
			require.Equal(t, test.complete, facts.Complete)
		})
	}
}

func TestResponsesUsageFinishIsIdempotent(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"}}
	accounting := NewResponsesUsageAccumulator(info)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_item.done", Item: &dto.ResponsesOutput{Type: dto.ResponsesOutputTypeImageGenerationCall, ID: "image_1", Status: "completed", Result: "image"}}, nil)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "The generated image is ready."}, nil)
	usage := accounting.Finish()
	require.Positive(t, usage.CompletionTokens)
	assert.Equal(t, "estimated_partial", usage.UsageSource)
	require.NotNil(t, usage.BillingUsage)
	assert.True(t, usage.BillingUsage.Estimated)
	assert.Equal(t, usage.CompletionTokens, effectiveBillingUsage(usage).CompletionTokens)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.failed"}, nil)
	assert.Same(t, usage, accounting.Finish())
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
}

// An image that completed before any terminal was delivered and bills once,
// whatever the terminal says. Other tool calls in the terminal snapshot bill
// only on a completed response, and image items without a final result never
// bill.
func TestResponsesTerminalImageBilling(t *testing.T) {
	image := dto.ResponsesOutput{ID: "image-1", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "completed", Result: "image-data"}
	for _, terminal := range []struct {
		eventType, status string
		wantSearches      int
	}{
		{"response.completed", "completed", 1},
		{"response.completed", "incomplete", 0},
		{"response.incomplete", "incomplete", 0},
		{"response.failed", "failed", 0},
		{"response.cancelled", "cancelled", 0},
	} {
		t.Run(terminal.eventType+"/"+terminal.status, func(t *testing.T) {
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.1"}}
			accounting := NewResponsesUsageAccumulator(info)
			accounting.Observe(&dto.ResponsesStreamResponse{Type: dto.ResponsesOutputTypeItemDone, Item: &image}, nil)
			accounting.Observe(&dto.ResponsesStreamResponse{Type: terminal.eventType, Response: &dto.OpenAIResponsesResponse{
				Status: []byte(`"` + terminal.status + `"`),
				Output: []dto.ResponsesOutput{
					image,
					{ID: "image-2", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "in_progress"},
					{ID: "search-1", Type: dto.BuildInCallWebSearchCall, Status: "completed"},
				},
			}}, nil)
			accounting.Finish()
			assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
			searches := 0
			if tool := info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview]; tool != nil {
				searches = tool.CallCount
			}
			assert.Equal(t, terminal.wantSearches, searches)
		})
	}
}

// Upstreams can deliver a final image still labelled in_progress on
// output_item.done and in the completed snapshot (Sub2API f2ca16577). The
// result reached the client, so it bills once, as in NewAPI.
func TestResponsesStreamBillsDeliveredInProgressImage(t *testing.T) {
	image := dto.ResponsesOutput{ID: "ig_1", Type: dto.ResponsesOutputTypeImageGenerationCall, Status: "in_progress", Result: "final-image"}
	index := 0
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-5.5"}}
	accounting := NewResponsesUsageAccumulator(info)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: dto.ResponsesOutputTypeItemDone, Item: &image, OutputIndex: &index}, nil)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{
		Status: []byte(`"completed"`),
		Output: []dto.ResponsesOutput{image},
	}}, nil)
	accounting.Finish()
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolImageGeneration)
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
}

func TestResponsesUsageEstimateRefreshesNativeBillingSnapshot(t *testing.T) {
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"}}
	accounting := NewResponsesUsageAccumulator(info)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.created", Response: &dto.OpenAIResponsesResponse{Usage: &dto.Usage{InputTokens: 100, InputTokensDetails: &dto.InputTokenDetails{CachedTokens: 10, AudioTokens: 2}}}}, nil)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "An interrupted response with a real output."}, nil)
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
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.created", Response: &dto.OpenAIResponsesResponse{Usage: &dto.Usage{InputTokens: 30, BillingUsage: authoritative}}}, nil)
	accounting.Observe(&dto.ResponsesStreamResponse{Type: "response.output_text.delta", Delta: "converted output"}, nil)
	usage := accounting.Finish()
	assert.Equal(t, authoritative, usage.BillingUsage)
	assert.NotSame(t, authoritative, usage.BillingUsage)
	assert.Equal(t, 12, effectiveBillingUsage(usage).CompletionTokens)
}
