package xai

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// xAI Responses reports its billed server-side tool counts in
// usage.server_side_tool_usage_details: web_search_calls replaces the counted
// web_search_call items under web_search, and x_search bills per fetched post
// and profile. A body without the details keeps the counted items.
func TestXaiResponsesToolUsage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want map[string]int
	}{
		{
			name: "non-stream body",
			raw:  `{"status":"completed","usage":{"input_tokens":3,"num_sources_used":9,"server_side_tool_usage_details":{"web_search_calls":2,"x_search_calls":1,"x_posts_fetched":7,"x_users_fetched":3}}}`,
			want: map[string]int{dto.BuildInToolWebSearch: 2, dto.BuildInToolWebSearchPreview: 0, "x_search_posts": 7, "x_search_profiles": 3},
		},
		{
			name: "terminal stream frame",
			raw:  `{"type":"response.completed","response":{"usage":{"server_side_tool_usage_details":{"web_search_calls":1}}}}`,
			want: map[string]int{dto.BuildInToolWebSearch: 1, dto.BuildInToolWebSearchPreview: 0},
		},
		{
			name: "negative count is ignored",
			raw:  `{"usage":{"server_side_tool_usage_details":{"web_search_calls":-4,"x_posts_fetched":2}}}`,
			want: map[string]int{dto.BuildInToolWebSearchPreview: 0, "x_search_posts": 2},
		},
		{
			name: "no tool usage details",
			raw:  `{"usage":{"input_tokens":3}}`,
			want: map[string]int{dto.BuildInToolWebSearchPreview: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses}
			(&Adaptor{}).Init(info)
			info.CountBillableToolCall(dto.BuildInCallWebSearchCall, "")
			info.ApplyVendorToolUsage([]byte(tt.raw))
			counts := map[string]int{}
			for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
				counts[name] = tool.CallCount
			}
			assert.Equal(t, tt.want, counts)
		})
	}
}

// Every key the xAI reader can bill is priced before send: x_search requires
// the post and profile prices, and web_search_calls bill under web_search even
// when the request declared web_search_preview.
func TestXaiResponsesToolPriceRequirements(t *testing.T) {
	info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeResponses, RelayFormat: types.RelayFormatOpenAIResponses}
	(&Adaptor{}).Init(info)
	for body, want := range map[string][]string{
		`{"tools":[{"type":"x_search"}]}`:           {"tool:x_search_posts", "tool:x_search_profiles"},
		`{"tools":[{"type":"web_search"}]}`:         {"tool:web_search"},
		`{"tools":[{"type":"web_search_preview"}]}`: {"tool:web_search", "tool:web_search_preview"},
	} {
		required, err := helper.PricingRequirementsFromBody(info, []byte(body))
		require.NoError(t, err)
		assert.Equal(t, want, required.Dimensions, body)
	}
}
