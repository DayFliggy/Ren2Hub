package openai_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type streamHandler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)

func routeStreamContext(t *testing.T, path string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set("route_observation_epoch", uint64(0))
	common.SetContextKey(c, constant.ContextKeyChannelId, 83001)
	service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 83001})
	return c, recorder
}

func routeAttemptEvidence(t *testing.T, c *gin.Context) gjson.Result {
	t.Helper()
	events, err := common.Marshal(service.RequestPolicy(c).Events())
	require.NoError(t, err)
	return gjson.GetBytes(events, "0.route_attempt")
}

// runRouteStream sends the frames through a real stream handler and returns
// the client bytes and the routing evidence of the attempt.
func runRouteStream(t *testing.T, path string, format types.RelayFormat, handler streamHandler, frames []string, done bool) (string, gjson.Result) {
	t.Helper()
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	c, recorder := routeStreamContext(t, path)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "empty-test"}, OriginModelName: "empty-test",
		IsStream: true, RelayFormat: format, DisablePing: true}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	if done {
		body.WriteString("data: [DONE]\n\n")
	}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}
	service.MarkRouteAttemptSent(c, info)
	_, apiErr := handler(c, info, resp)
	require.Nil(t, apiErr)
	service.FinishRouteAttempt(c, info, apiErr)
	return recorder.Body.String(), routeAttemptEvidence(t, c)
}

func chatChunk(delta string, finish string) string {
	reason := "null"
	if finish != "" {
		reason = `"` + finish + `"`
	}
	return `{"id":"chatcmpl-e","object":"chat.completion.chunk","created":1710000000,"model":"empty-test","choices":[{"index":0,"delta":` + delta + `,"finish_reason":` + reason + `}]}`
}

// The empty-stream judgment only changes routing evidence: the real chat
// stream handler writes the same client bytes as before. A stream counts as
// empty only after a normal completion marker without content or output usage.
func TestRouteEvidenceMarksEmptyStream(t *testing.T) {
	role := chatChunk(`{"role":"assistant","content":""}`, "")
	stop := chatChunk(`{}`, "stop")
	usage := `{"id":"chatcmpl-e","object":"chat.completion.chunk","created":1710000000,"model":"empty-test","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":7,"total_tokens":16}}`
	// prompt_cache_hit_tokens disagrees with cached_tokens; the output count is still reported.
	conflictingCache := `{"id":"chatcmpl-e","object":"chat.completion.chunk","created":1710000000,"model":"empty-test","choices":[],"usage":{"prompt_tokens":9,"completion_tokens":7,"total_tokens":16,"prompt_tokens_details":{"cached_tokens":3},"prompt_cache_hit_tokens":5}}`
	audio := chatChunk(`{"audio":{"id":"audio_1","data":"UklGRg==","transcript":"hi"}}`, "")
	audioMeta := chatChunk(`{"audio":{"id":"audio_1","expires_at":1710003600}}`, "")
	refusal := chatChunk(`{"refusal":"I can't help with that."}`, "")
	for _, tc := range []struct {
		name, outcome, reason string
		frames, client        []string
	}{
		{name: "role delta then stop", outcome: "failure", reason: "empty_stream", frames: []string{role, stop}, client: []string{role, stop}},
		// Without stream_options.include_usage the usage frame is not forwarded.
		{name: "upstream reports output tokens", outcome: "ignored", reason: "no_visible_output", frames: []string{role, stop, usage}, client: []string{role, stop}},
		{name: "output tokens with conflicting cache counts", outcome: "ignored", reason: "no_visible_output", frames: []string{role, stop, conflictingCache}, client: []string{role, stop}},
		{name: "audio only", outcome: "success", frames: []string{role, audio, stop}, client: []string{role, audio, stop}},
		{name: "refusal only", outcome: "success", frames: []string{role, refusal, stop}, client: []string{role, refusal, stop}},
		{name: "audio metadata only", outcome: "failure", reason: "empty_stream", frames: []string{role, audioMeta, stop}, client: []string{role, audioMeta, stop}},
		{name: "no completion marker", outcome: "ignored", reason: "no_visible_output", frames: []string{role}, client: []string{role}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, ev := runRouteStream(t, "/v1/chat/completions", types.RelayFormatOpenAI, openai.OaiStreamHandler, tc.frames, true)
			want := ""
			for _, frame := range tc.client {
				want += "data: " + frame + "\n\n"
			}
			require.Equal(t, want+"data: [DONE]\n\n", body, "the client bytes do not depend on the routing outcome")
			require.Equal(t, tc.outcome, ev.Get("outcome").String())
			require.Equal(t, tc.reason, ev.Get("outcome_reason").String())
		})
	}
}

// A Responses stream whose only output is a hosted tool call served the request.
func TestRouteEvidenceCountsHostedToolResponses(t *testing.T) {
	item := `{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"weather"}}`
	frames := []string{
		`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response","status":"in_progress","output":[]}}`,
		`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":` + item + `}`,
		`{"type":"response.output_item.done","sequence_number":2,"output_index":0,"item":` + item + `}`,
		`{"type":"response.completed","sequence_number":3,"response":{"id":"resp_1","object":"response","status":"completed","output":[` + item + `]}}`,
	}
	_, ev := runRouteStream(t, "/v1/responses", types.RelayFormatOpenAIResponses, openai.OaiResponsesStreamHandler, frames, false)
	require.Equal(t, "success", ev.Get("outcome").String())
	require.Empty(t, ev.Get("outcome_reason").String())
}

// A realtime connection is not one generation. A session that only connects
// and closes normally keeps the native result and is never an empty stream.
func TestRouteEvidenceIgnoresRealtimeConnectionLifecycle(t *testing.T) {
	upgrader := websocket.Upgrader{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.created","event_id":"event_1","session":{"id":"sess_1","model":"empty-test"}}`))
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		time.Sleep(50 * time.Millisecond)
	}))
	t.Cleanup(target.Close)
	accepted := make(chan *websocket.Conn, 1)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted <- conn
		<-r.Context().Done()
	}))
	t.Cleanup(gateway.Close)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	clientWs := <-accepted
	targetWs, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(target.URL, "http"), nil)
	require.NoError(t, err)

	c, _ := routeStreamContext(t, "/v1/realtime")
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "empty-test"}, OriginModelName: "empty-test",
		RelayMode: relayconstant.RelayModeRealtime, RelayFormat: types.RelayFormatOpenAIRealtime, ClientWs: clientWs, TargetWs: targetWs}
	service.MarkRouteAttemptSent(c, info)
	apiErr, _ := openai.OpenaiRealtimeHandler(c, info)
	require.Nil(t, apiErr)
	service.FinishRouteAttempt(c, info, nil)
	ev := routeAttemptEvidence(t, c)
	require.Equal(t, "success", ev.Get("outcome").String())
	require.Empty(t, ev.Get("outcome_reason").String())
}
