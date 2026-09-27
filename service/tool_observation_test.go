package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestResponsesToolCallsDeduplicateTerminalSnapshots(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	acc := NewResponsesUsageAccumulator(info)
	item := dto.ResponsesOutput{ID: "search-one", Type: dto.BuildInCallWebSearchCall, Status: "completed"}
	index := 0
	event := &dto.ResponsesStreamResponse{Type: dto.ResponsesOutputTypeItemDone, Item: &item, OutputIndex: &index}
	acc.Observe(event)
	acc.Observe(event)
	acc.Observe(&dto.ResponsesStreamResponse{Type: "response.completed", Response: &dto.OpenAIResponsesResponse{Output: []dto.ResponsesOutput{item}}})
	require.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearchPreview].CallCount)
	require.False(t, info.BillingToolUsageInvalid)
}

func TestResponsesConflictingToolIdentityCannotHideCharge(t *testing.T) {
	info := &relaycommon.RelayInfo{}
	index := 0
	info.ObserveBillableToolCall(&dto.ResponsesOutput{ID: "one", Type: dto.BuildInCallWebSearchCall, Status: "completed"}, &index)
	info.ObserveBillableToolCall(&dto.ResponsesOutput{ID: "two", Type: dto.BuildInCallFileSearchCall, Status: "completed"}, &index)
	require.True(t, info.BillingToolUsageInvalid)
}
