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
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	info := &relaycommon.RelayInfo{OriginModelName: "claude-3-7-sonnet"}
	quote, err := billing.NewQuote(billing.PriceVersion{Model: info.OriginModelName, Mode: billing.ModePerCall, Pricing: billing.PricingValues{"ModelPrice": "0"}, Prices: []billing.UnitPrice{{Quantity: "tool:stream_fn", Unit: "per_call", Price: "0.002"}}}, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	info.PriceData.Quote = quote

	countClaudeStreamBillableTools(c, info, &dto.ClaudeResponse{
		Type: "message_delta",
		Usage: &dto.ClaudeUsage{
			ServerToolUse: &dto.ClaudeServerToolUse{WebSearchRequests: 3},
		},
	})
	assert.Equal(t, 3, c.GetInt("claude_web_search_requests"))

	countClaudeStreamBillableTools(c, info, &dto.ClaudeResponse{
		Type: "content_block_start",
		ContentBlock: &dto.ClaudeMediaMessage{
			Type: "tool_use",
			Name: "stream_fn",
		},
	})
	require.Contains(t, info.ResponsesUsageInfo.BuiltInTools, "stream_fn")
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools["stream_fn"].CallCount)
}
