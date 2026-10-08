package ali

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// DashScope Responses moved to /compatible-mode/v1/responses, bills web search
// like the agent strategy and reports the count in usage.x_tools; it replaces
// the web_search_call items counted under the protocol keys, and a declared
// web search requires the agent price before send.
func TestAliResponsesWebSearchUsage(t *testing.T) {
	for _, raw := range []string{
		`{"status":"completed","usage":{"x_tools":{"web_search":{"count":2}}}}`,
		`{"type":"response.completed","response":{"usage":{"x_tools":{"web_search":{"count":2}}}}}`,
	} {
		info := &relaycommon.RelayInfo{
			RelayMode:   constant.RelayModeResponses,
			RelayFormat: types.RelayFormatOpenAIResponses,
			ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://dashscope.invalid"},
		}
		adaptor := &Adaptor{}
		adaptor.Init(info)
		url, err := adaptor.GetRequestURL(info)
		require.NoError(t, err)
		assert.Equal(t, "https://dashscope.invalid/compatible-mode/v1/responses", url)
		required, err := relayhelper.PricingRequirementsFromBody(info, []byte(`{"tools":[{"type":"web_search"}]}`))
		require.NoError(t, err)
		assert.Equal(t, []string{"tool:search_strategy_agent", "tool:web_search"}, required.Dimensions)

		info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
		info.ApplyVendorToolUsage([]byte(raw))
		counts := map[string]int{}
		for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
			counts[name] = tool.CallCount
		}
		assert.Equal(t, map[string]int{aliSearchAgentKey: 2, dto.BuildInToolWebSearch: 0, dto.BuildInToolWebSearchPreview: 0}, counts, raw)
	}
}

// Claude clients on DashScope's Anthropic-compatible models bill search under
// the agent key and get the cc_entrypoint system marker block DashScope
// requires before it searches (block form verified upstream 2026-10-05; a
// plain string system did not search). Models converted to compatible-mode
// Chat keep the protocol key.
func TestConvertClaudeRequestWebSearchEntrypoint(t *testing.T) {
	t.Setenv(aliAnthropicMessagesModelsEnv, defaultAliAnthropicMessagesModels)
	const (
		anthropicModel = "qwen3.8-flash"
		marker         = `{"type":"text","text":"x-anthropic-billing-header: cc_entrypoint=cli;"}`
		messages       = `"messages":[{"role":"user","content":"Weather?"}]`
		searchTool     = `{"type":"web_search_20250305","name":"web_search"}`
	)
	tests := []struct {
		name string
		body string // Claude request fields after model and max_tokens
		want string // raw JSON of system
	}{
		{"marker without system", messages + `,"tools":[` + searchTool + `]`, `[` + marker + `]`},
		{"marker before string system", `"system":"Be brief.",` + messages + `,"tools":[` + searchTool + `]`, `[` + marker + `,{"type":"text","text":"Be brief."}]`},
		{"marker before array system", `"system":[{"type":"text","text":"Be brief."}],` + messages + `,"tools":[` + searchTool + `]`, `[` + marker + `,{"type":"text","text":"Be brief."}]`},
		{"marker for empty array system", `"system":[],` + messages + `,"tools":[` + searchTool + `]`, `[` + marker + `]`},
		{"marker already sent by the client", `"system":"x-anthropic-billing-header: cc_version=2.1; cc_entrypoint=cli;",` + messages + `,"tools":[` + searchTool + `]`, `"x-anthropic-billing-header: cc_version=2.1; cc_entrypoint=cli;"`},
		{"no marker without a search tool", `"system":"Be brief.",` + messages + `,"tools":[{"name":"f","input_schema":{"type":"object"}}]`, `"Be brief."`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{
				RelayFormat:     types.RelayFormatClaude,
				OriginModelName: anthropicModel,
				ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: anthropicModel},
			}
			adaptor := &Adaptor{}
			adaptor.Init(info)
			assert.Equal(t, aliSearchAgentKey, info.WebSearchBillingKey)
			var request dto.ClaudeRequest
			require.NoError(t, common.UnmarshalJsonStr(`{"model":"`+anthropicModel+`","max_tokens":64,`+tt.body+`}`, &request))
			var converted any
			for range 2 { // converting the same request twice adds one marker
				var err error
				converted, err = adaptor.ConvertClaudeRequest(c, info, &request)
				require.NoError(t, err)
			}
			wire, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, gjson.GetBytes(wire, "system").Raw)
		})
	}

	t.Run("billing keys", func(t *testing.T) {
		for model, wantKey := range map[string]string{anthropicModel: aliSearchAgentKey, "llama-4-scout": ""} {
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: model}}
			(&Adaptor{}).Init(info)
			assert.Equal(t, wantKey, info.WebSearchBillingKey, model)
			required, err := relayhelper.PricingRequirementsFromBody(info, []byte(`{"tools":[`+searchTool+`]}`))
			require.NoError(t, err)
			want := []string{"tool:web_search"}
			if wantKey != "" {
				want = []string{"tool:search_strategy_agent", "tool:web_search"}
			}
			assert.Equal(t, want, required.Dimensions, model)
		}
	})
}
