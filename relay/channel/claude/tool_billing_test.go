package claude

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"time"
)

func TestHandleClaudeResponseDataCountsToolUse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-3-7-sonnet",
		RelayFormat:     types.RelayFormatClaude,
	}
	quote, quoteErr := billing.NewQuote(billing.PriceVersion{Model: info.OriginModelName, Mode: billing.ModePerCall, Pricing: billing.PricingValues{"ModelPrice": "0"}, Prices: []billing.UnitPrice{{Quantity: "tool:lookup_fn", Unit: "per_call", Price: "0.003"}}}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, quoteErr)
	info.PriceData.Quote = quote
	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}

	data := []byte(`{
		"type":"message",
		"content":[
			{"type":"text","text":"hi"},
			{"type":"tool_use","id":"tu1","name":"lookup_fn","input":{}},
			{"type":"server_tool_use","id":"stu1","name":"web_search","input":{}}
		],
		"usage":{"input_tokens":1,"output_tokens":1}
	}`)

	err := HandleClaudeResponseData(c, info, claudeInfo, nil, data)
	require.Nil(t, err)
	require.NotNil(t, info.ResponsesUsageInfo)
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, "lookup_fn")
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["lookup_fn"].CallCount)
	assert.NotContains(t, info.ResponsesUsageInfo.BuiltInTools, "web_search")
}

func TestCountClaudeStreamBillableToolsSetsWebSearchRequests(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "claude-3-7-sonnet"}
	quote, err := billing.NewQuote(billing.PriceVersion{Model: info.OriginModelName, Mode: billing.ModePerCall, Pricing: billing.PricingValues{"ModelPrice": "0"}, Prices: []billing.UnitPrice{{Quantity: "tool:stream_fn", Unit: "per_call", Price: "0.002"}}}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	info.PriceData.Quote = quote

	countClaudeStreamBillableTools(info, &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			ServerToolUse: &dto.ClaudeServerToolUse{WebSearchRequests: 3},
		},
	})
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, dto.BuildInToolWebSearch)
	assert.Equal(t, 3, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolWebSearch].CallCount)
	assert.Len(t, info.ResponsesUsageInfo.BuiltInTools, 1, "server tools reported as zero are not recorded")

	countClaudeStreamBillableTools(info, &dto.ClaudeResponse{
		Type: "content_block_start",
		ContentBlock: &dto.ClaudeMediaMessage{
			Type: "tool_use",
			Name: "stream_fn",
		},
	})
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, "stream_fn")
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["stream_fn"].CallCount)
}

// usage.server_tool_use counts bill under each server tool's own key, and a
// later cumulative message_delta replaces the earlier report.
func TestClaudeServerToolUseKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "claude-3-7-sonnet", RelayFormat: types.RelayFormatClaude}
	countClaudeStreamBillableTools(info, &dto.ClaudeResponse{
		Type:  "message_delta",
		Usage: &dto.ClaudeUsage{ServerToolUse: &dto.ClaudeServerToolUse{WebSearchRequests: 1, WebFetchRequests: 1}},
	})
	countClaudeStreamBillableTools(info, &dto.ClaudeResponse{
		Type:  "message_delta",
		Usage: &dto.ClaudeUsage{ServerToolUse: &dto.ClaudeServerToolUse{WebSearchRequests: 1, WebFetchRequests: 2, CodeExecutionRequests: 3, ToolSearchRequests: 4}},
	})
	want := map[string]int{"web_search": 1, "web_fetch": 2, "code_execution": 3, "tool_search": 4}
	assert.Equal(t, want, claudeToolCounts(info))

	nonStream := &relaycommon.RelayInfo{OriginModelName: "claude-3-7-sonnet", RelayFormat: types.RelayFormatClaude}
	data := []byte(`{"type":"message","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":1,"output_tokens":1,"server_tool_use":{"web_search_requests":1,"web_fetch_requests":2,"code_execution_requests":3,"tool_search_requests":4}}}`)
	require.Nil(t, HandleClaudeResponseData(c, nonStream, &ClaudeResponseInfo{Usage: &dto.Usage{}}, nil, data))
	assert.Equal(t, want, claudeToolCounts(nonStream))
}

func claudeToolCounts(info *relaycommon.RelayInfo) map[string]int {
	counts := map[string]int{}
	for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
		counts[name] = tool.CallCount
	}
	return counts
}
