package openai_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// The empty-stream judgment only changes routing evidence: the real chat
// stream handler writes the same client bytes as before.
func TestRouteEvidenceMarksEmptyStream(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	role := `{"id":"chatcmpl-e","object":"chat.completion.chunk","created":1710000000,"model":"empty-test","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`
	stop := `{"id":"chatcmpl-e","object":"chat.completion.chunk","created":1710000000,"model":"empty-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	usage := `{"id":"chatcmpl-e","object":"chat.completion.chunk","created":1710000000,"model":"empty-test","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":7,"total_tokens":16}}`
	for _, tc := range []struct {
		name, outcome, reason string
		frames, client        []string
	}{
		{name: "role delta then stop", outcome: "failure", reason: "empty_stream", frames: []string{role, stop}, client: []string{role, stop}},
		// Without stream_options.include_usage the usage frame is not forwarded.
		{name: "upstream reports output tokens", outcome: "ignored", reason: "no_visible_output", frames: []string{role, stop, usage}, client: []string{role, stop}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("route_observation_epoch", uint64(0))
			common.SetContextKey(c, constant.ContextKeyChannelId, 83001)
			service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 83001})
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "empty-test"}, OriginModelName: "empty-test",
				IsStream: true, RelayFormat: types.RelayFormatOpenAI, DisablePing: true}
			var body strings.Builder
			for _, frame := range tc.frames {
				body.WriteString("data: " + frame + "\n\n")
			}
			body.WriteString("data: [DONE]\n\n")
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}

			service.MarkRouteAttemptSent(c, info)
			_, apiErr := openai.OaiStreamHandler(c, info, resp)
			require.Nil(t, apiErr)
			service.FinishRouteAttempt(c, info, apiErr)

			want := ""
			for _, frame := range tc.client {
				want += "data: " + frame + "\n\n"
			}
			require.Equal(t, want+"data: [DONE]\n\n", recorder.Body.String(), "the client bytes do not depend on the routing outcome")
			events, err := common.Marshal(service.RequestPolicy(c).Events())
			require.NoError(t, err)
			ev := gjson.GetBytes(events, "0.route_attempt")
			require.Equal(t, tc.outcome, ev.Get("outcome").String())
			require.Equal(t, tc.reason, ev.Get("outcome_reason").String())
		})
	}
}
