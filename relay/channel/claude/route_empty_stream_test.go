package claude_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A Claude stream that hits a stop sequence the request named before any text
// legitimately returns no content: the request caused the end, so it is not an
// empty stream. The same empty stream ending at end_turn without a custom stop
// sequence is still one. Both go through the chat-to-Claude handler.
func TestRouteEvidenceClaudeStopSequenceIsRequestTerminated(t *testing.T) {
	start := `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"empty-test","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":9,"output_tokens":0}}}`
	stop := `{"type":"message_stop"}`
	for _, tc := range []struct {
		name, delta, outcome, reason string
	}{
		{"stop sequence before any text", `{"type":"message_delta","delta":{"stop_reason":"stop_sequence","stop_sequence":"END"},"usage":{"output_tokens":0}}`, "ignored", "request_terminated"},
		{"end turn without text", `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":0}}`, "failure", "empty_stream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := runClaudeRouteStream(t, start, tc.delta, stop)
			require.Equal(t, tc.outcome, ev.Get("outcome").String(), ev.Raw)
			require.Equal(t, tc.reason, ev.Get("outcome_reason").String())
		})
	}
}

func runClaudeRouteStream(t *testing.T, frames ...string) gjson.Result {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("route_observation_epoch", uint64(0))
	common.SetContextKey(c, constant.ContextKeyChannelId, 83003)
	service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 83003})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "empty-test"}, OriginModelName: "empty-test",
		IsStream: true, RelayFormat: types.RelayFormatOpenAI, DisablePing: true}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}
	service.MarkRouteAttemptSent(c, info)
	_, apiErr := claude.ClaudeStreamHandler(c, resp, info)
	require.Nil(t, apiErr)
	service.FinishRouteAttempt(c, info, apiErr)
	require.Contains(t, recorder.Body.String(), "data: [DONE]", "the client receives the converted chat stream")
	events, err := common.Marshal(service.RequestPolicy(c).Events())
	require.NoError(t, err)
	return gjson.GetBytes(events, "0.route_attempt")
}
