package common

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureResponseUsageRetainsEstimateOrigin(t *testing.T) {
	info := &RelayInfo{}
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12, UsageSource: dto.BillingUsageSourceGeminiChat}
	usage.BillingUsage = dto.NewEstimatedGeminiChatBillingUsage(usage)
	info.CaptureResponseUsage(usage, "text", nil)
	assert.Equal(t, "estimated", info.ResponseUsage.UsageSource)
	info.CaptureResponseUsage(&dto.Usage{UsageSource: "provider_and_estimated"}, "realtime", nil)
	assert.Equal(t, "provider_and_estimated", info.ResponseUsage.UsageSource)
	info.CaptureResponseUsage(&dto.Usage{PromptTokens: 1}, "text", nil)
	assert.Equal(t, "provider", info.ResponseUsage.UsageSource)
}

func TestAttemptResetRetainsLogicalBillingAndDropsUndeliveredToolUsage(t *testing.T) {
	billing := &struct{ BillingSettler }{}
	info := &RelayInfo{
		RequestId: "logical-request", Billing: billing, FinalPreConsumedQuota: 100,
		StartTime: time.Now(), RelayFormat: types.RelayFormatClaude,
		ClaudeConvertInfo:     &ClaudeConvertInfo{MessageStarted: true},
		UpstreamSideEffecting: true, UpstreamPreparationOnly: true,
		ResponsesUsageInfo: &ResponsesUsageInfo{BuiltInTools: map[string]*BuildInToolInfo{
			dto.BuildInToolWebSearch: {ToolName: dto.BuildInToolWebSearch, CallCount: 2, SearchContextSize: "low"},
		}},
	}
	require.NoError(t, info.ResetAttemptState())
	assert.Same(t, billing, info.Billing)
	assert.Equal(t, "logical-request", info.RequestId)
	assert.Equal(t, 100, info.FinalPreConsumedQuota)
	assert.False(t, info.EnsureClaudeConvertInfo().MessageStarted)
	assert.False(t, info.UpstreamSideEffecting)
	assert.False(t, info.UpstreamPreparationOnly, "a new attempt cannot inherit proof that generation has not started")
	assert.Zero(t, info.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
	assert.Equal(t, "low", info.BuiltInTools[dto.BuildInToolWebSearch].SearchContextSize)
	info.MarkValidOutput()
	require.ErrorContains(t, info.ResetAttemptState(), "after output delivery")
	assert.True(t, info.HasValidOutput())
}
