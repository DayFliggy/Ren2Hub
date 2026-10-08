package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Azure Responses bills web search by tool_usage.web_search.num_requests (Bing
// transactions) under bing_web_search, and the explicit zeros silence the
// web_search_call items, which leaves one surcharge row. A declared web search
// therefore also requires the bing_web_search price before send. OpenAI keeps
// billing the web_search_call items under the protocol key.
func TestAzureResponsesWebSearchUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})

	const usage = `"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13},"tool_usage":{"web_search":{"num_requests":5}}`
	var output, itemsDone strings.Builder
	for _, id := range []string{"ws_1", "ws_2", "ws_3"} {
		item := `{"type":"web_search_call","id":"` + id + `","status":"completed","action":{"type":"search","query":"go"}}`
		output.WriteString(item + ",")
		itemsDone.WriteString(`data: {"type":"response.output_item.done","item":` + item + "}\n\n")
	}
	output.WriteString(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Go 1.25"}]}`)
	response := `{"id":"resp_1","object":"response","model":"gpt-5.1","status":"completed","output":[` + output.String() + `],` + usage + `}`
	bodies := map[string]string{
		"application/json":  response,
		"text/event-stream": itemsDone.String() + `data: {"type":"response.completed","response":` + response + "}\n\ndata: [DONE]\n\n",
	}

	for _, channel := range []struct {
		channelType  int
		wantBilled   map[string]int
		wantRequired []string
	}{
		{constant.ChannelTypeAzure, map[string]int{"bing_web_search": 5}, []string{"tool:bing_web_search", "tool:web_search"}},
		{constant.ChannelTypeOpenAI, map[string]int{"web_search": 3}, []string{"tool:web_search"}},
	} {
		for contentType, body := range bodies {
			t.Run(fmt.Sprintf("channel %d %s", channel.channelType, contentType), func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{
					RelayMode:       relayconstant.RelayModeResponses,
					RelayFormat:     types.RelayFormatOpenAIResponses,
					OriginModelName: "gpt-5.1",
					IsStream:        contentType == "text/event-stream",
					DisablePing:     true,
					ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: channel.channelType, UpstreamModelName: "gpt-5.1"},
					// GenRelayInfoResponses records the declared hosted tool type.
					ResponsesUsageInfo: &relaycommon.ResponsesUsageInfo{BuiltInTools: map[string]*relaycommon.BuildInToolInfo{
						dto.BuildInToolWebSearch: {ToolName: dto.BuildInToolWebSearch},
					}},
				}
				adaptor := &Adaptor{}
				adaptor.Init(info)
				required, err := helper.PricingRequirementsFromBody(info, []byte(`{"model":"gpt-5.1","tools":[{"type":"web_search"}]}`))
				require.NoError(t, err)
				assert.Equal(t, channel.wantRequired, required.Dimensions)

				resp := &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     http.Header{"Content-Type": []string{contentType}},
				}
				var apiErr *types.NewAPIError
				if info.IsStream {
					_, apiErr = OaiResponsesStreamHandler(c, info, resp)
				} else {
					_, apiErr = OaiResponsesHandler(c, info, resp)
				}
				require.Nil(t, apiErr)
				billed := map[string]int{}
				for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
					if tool.CallCount > 0 {
						billed[name] = tool.CallCount
					}
				}
				assert.Equal(t, channel.wantBilled, billed)
			})
		}
	}
}
