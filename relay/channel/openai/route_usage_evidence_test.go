package openai_test

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouteUsageFromRealHandlers(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	tests := []struct {
		name, path, body string
		stream           bool
		known            bool
		input, read      int64
	}{
		{"openai", "/v1/responses", `{"id":"resp_1","status":"completed","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":0},"output_tokens":5}}`, false, true, 100, 0},
		{"openai missing", "/v1/responses", `{"id":"resp_2","status":"completed","usage":{"input_tokens":100,"output_tokens":5}}`, false, false, 100, 0},
		{"claude", "/v1/messages", `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":15,"cache_creation_input_tokens":5,"cache_read_input_tokens":80,"output_tokens":5}}`, false, true, 100, 80},
		{"gemini", "/v1beta/models/m:generateContent", `{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":5,"cachedContentTokenCount":80}}`, false, true, 100, 80},
		{"responses stream", "/v1/responses", "data: " + `{"type":"response.output_text.delta","delta":"hi"}` + "\n\ndata: " + `{"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":80},"output_tokens":5}}}` + "\n\n", true, true, 100, 80},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tc.path, nil)
			c.Set("route_observation_epoch", uint64(0))
			common.SetContextKey(c, constant.ContextKeyChannelId, 17)
			service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 17})
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "usage-test"}, OriginModelName: "usage-test", IsStream: tc.stream}
			resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}
			service.MarkRouteAttemptSent(c, info)
			var apiErr *types.NewAPIError
			switch {
			case tc.name == "gemini":
				_, apiErr = gemini.GeminiTextGenerationHandler(c, info, resp)
			case tc.name == "claude":
				info.RelayFormat = types.RelayFormatClaude
				_, apiErr = claude.ClaudeHandler(c, resp, info)
			case tc.stream:
				_, apiErr = openai.OaiResponsesStreamHandler(c, info, resp)
			default:
				_, apiErr = openai.OaiResponsesHandler(c, info, resp)
			}
			require.Nil(t, apiErr)
			service.FinishRouteAttempt(c, info, apiErr)
			events, err := common.Marshal(service.RequestPolicy(c).Events())
			require.NoError(t, err)
			ev := gjson.GetBytes(events, "0.route_attempt")
			require.True(t, ev.Exists(), "attempt evidence absent")
			require.Equal(t, tc.known, ev.Get("cache_known").Bool())
			require.Equal(t, tc.input, ev.Get("input_tokens").Int())
			require.Equal(t, tc.read, ev.Get("cache_read_tokens").Int())
			require.Equal(t, "upstream", ev.Get("output_source").String())
		})
	}
}
