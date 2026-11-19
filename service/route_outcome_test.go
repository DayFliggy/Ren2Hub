package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// upstreamError builds the relay error the way adaptors do for a non-2xx reply.
func upstreamError(t *testing.T, status int, body string, showBody bool) *types.NewAPIError {
	t.Helper()
	resp := &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	apiErr := RelayErrorHandler(context.Background(), resp, showBody)
	require.NotNil(t, apiErr)
	return apiErr
}

// finishRouteAttempt sends one attempt on channel 11 and returns its evidence.
func finishRouteAttempt(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, frames ...string) *RouteAttemptEvidence {
	t.Helper()
	RequestPolicy(c).BeginAttempt(&model.Channel{Id: 11})
	MarkRouteAttemptSent(c, info)
	for _, frame := range frames {
		ObserveRouteStreamOutput(c, frame)
	}
	FinishRouteAttempt(c, info, apiErr)
	events := RequestPolicy(c).Events()
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Decision.Action == "attempt" {
			require.NotNil(t, events[i].RouteAttempt, "every sent attempt carries evidence")
			return events[i].RouteAttempt
		}
	}
	t.Fatal("no attempt event")
	return nil
}

func routeQualitySampleCount(id int) int {
	routeQualities.mu.Lock()
	defer routeQualities.mu.Unlock()
	total := 0
	for key, history := range routeQualities.history {
		if key.id != id {
			continue
		}
		for _, bucket := range history.buckets {
			total += int(bucket.total)
		}
	}
	return total
}

func routeOutcomeTestContext(t *testing.T) *gin.Context {
	t.Helper()
	setupRouteTest(t)
	resetRouteObservations(t)
	routeQualities.invalidate(11)
	t.Cleanup(func() { routeQualities.invalidate(11) })
	c := routeTestContext(t)
	BeginRouteObservation(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	return c
}

// A reseller that wraps a context overflow in a code-less 500 is reporting a
// request that no channel can serve; it must not open the channel's breaker.
// Credential and rate signals and text outside the error object still count.
func TestRouteOutcomeIgnoresWrappedContextOverflow(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, StartTime: time.Now()}
	key := routeBreakerKey{11, "route-model"}
	overflow := `{"error":{"message":"This model's maximum context length is 128000 tokens. However, your messages resulted in 130211 tokens."}}`

	t.Run("wrapped overflow never opens", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		for range routeBreakerOpenFailures + 1 {
			ev := finishRouteAttempt(t, c, info, upstreamError(t, http.StatusInternalServerError, overflow, false))
			require.Equal(t, "ignored", ev.Outcome)
			require.Equal(t, "request_context_overflow", ev.OutcomeReason)
		}
		class, _ := routeObservations.availability(key)
		require.Equal(t, routeAvailable, class)
		require.Zero(t, routeObservations.breakers[key].failures)
		require.Zero(t, routeQualitySampleCount(11), "no quality sample")
		require.Empty(t, routeObservations.snapshot(), "no observation sample")
	})

	t.Run("a plain internal error opens on the fifth", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		for i := range routeBreakerOpenFailures {
			class, _ := routeObservations.availability(key)
			require.Equal(t, routeAvailable, class, "attempt %d", i+1)
			ev := finishRouteAttempt(t, c, info, upstreamError(t, http.StatusInternalServerError, `{"error":{"message":"internal error"}}`, false))
			require.Equal(t, "failure", ev.Outcome)
			require.Empty(t, ev.OutcomeReason)
		}
		class, _ := routeObservations.availability(key)
		require.Equal(t, routeOpen, class)
	})

	for _, tc := range []struct {
		name     string
		status   int
		body     string
		showBody bool
	}{
		{"credential status keeps counting", http.StatusUnauthorized, overflow, false},
		{"rate limit keeps counting", http.StatusTooManyRequests, overflow, false},
		{"phrase outside the error object", http.StatusInternalServerError, `{"error":{"message":"upstream failed"},"detail":"maximum context length is 8192 tokens"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			ev := finishRouteAttempt(t, c, info, upstreamError(t, tc.status, tc.body, tc.showBody))
			require.Equal(t, "failure", ev.Outcome)
			require.Empty(t, ev.OutcomeReason)
			require.Equal(t, 1, routeObservations.breakers[key].failures)
		})
	}

	for _, message := range []string{
		"prompt is too long: 212034 tokens > 200000 maximum",
		"The input token count (1203411) exceeds the maximum number of tokens allowed (1048576).",
		"Request exceeds the context window of this model",
		"context length too long for the selected model",
		"Error code context_length_exceeded from provider",
		"max context length reached",
	} {
		t.Run(message, func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			body, err := common.Marshal(map[string]any{"error": map[string]any{"message": message}})
			require.NoError(t, err)
			ev := finishRouteAttempt(t, c, info, upstreamError(t, http.StatusBadGateway, string(body), false))
			require.Equal(t, "ignored", ev.Outcome)
			require.Equal(t, "request_context_overflow", ev.OutcomeReason)
		})
	}
}

// A stream that ends normally without one content event served nothing. It is
// a channel failure unless the upstream reported output the content check did
// not see, or ended the stream for a reason the request caused.
func TestEmptyCompletedStreamIsRouteFailure(t *testing.T) {
	key := routeBreakerKey{11, "route-model"}
	newInfo := func() *relaycommon.RelayInfo {
		status := relaycommon.NewStreamStatus()
		status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
		return &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, IsStream: true, StartTime: time.Now(), StreamStatus: status}
	}
	roleOnly := `{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`
	stop := `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`

	t.Run("role delta and stop opens after five", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		for range routeBreakerOpenFailures {
			ev := finishRouteAttempt(t, c, newInfo(), nil, roleOnly, stop)
			require.Equal(t, "failure", ev.Outcome)
			require.Equal(t, "empty_stream", ev.OutcomeReason)
		}
		class, _ := routeObservations.availability(key)
		require.Equal(t, routeOpen, class)
	})

	for _, tc := range []struct {
		name, dialect, usage, outcome, reason string
		frames                                []string
		status                                func(*relaycommon.StreamStatus)
	}{
		{name: "upstream reported output tokens", dialect: "openai", usage: `{"usage":{"prompt_tokens":5,"completion_tokens":7}}`, outcome: "ignored", reason: "no_visible_output", frames: []string{roleOnly, stop}},
		{name: "content filter", outcome: "ignored", reason: "request_terminated", frames: []string{roleOnly, `{"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`}},
		{name: "length limit", outcome: "ignored", reason: "request_terminated", frames: []string{roleOnly, `{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`}},
		{name: "tool call delta", outcome: "success", frames: []string{`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":""}}]}}]}`, `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`}},
		{name: "no observed event", outcome: "success"},
		{name: "image stream", outcome: "success", frames: []string{`{"type":"image_generation.partial_image","b64_json":"aW1n","partial_image_index":0}`, `[DONE]`}},
		{name: "speech stream", outcome: "success", frames: []string{`{"type":"speech.audio.delta","audio":"UklGRg=="}`, `{"type":"speech.audio.done","usage":{"output_tokens":4}}`}},
		{name: "responses completed with empty output", dialect: "openai", usage: `{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":0}}}`, outcome: "failure", reason: "empty_stream",
			frames: []string{`{"type":"response.created","response":{"status":"in_progress","output":[]}}`, `{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":0}}}`},
			status: (*relaycommon.StreamStatus).MarkCompleted},
		{name: "claude end turn without output", dialect: "claude", usage: `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`, outcome: "failure", reason: "empty_stream",
			frames: []string{`{"type":"message_start","message":{"role":"assistant","content":[]}}`, `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`, `{"type":"message_stop"}`}},
		{name: "claude refusal", dialect: "claude", usage: `{"type":"message_delta","delta":{"stop_reason":"refusal"},"usage":{"output_tokens":0}}`, outcome: "ignored", reason: "request_terminated",
			frames: []string{`{"type":"message_start","message":{"role":"assistant","content":[]}}`, `{"type":"message_delta","delta":{"stop_reason":"refusal"},"usage":{"output_tokens":0}}`, `{"type":"message_stop"}`}},
		{name: "gemini safety block", outcome: "ignored", reason: "request_terminated", frames: []string{`{"candidates":[{"content":{"parts":[]},"finishReason":"SAFETY"}]}`}},
		{name: "gemini stop without output", outcome: "failure", reason: "empty_stream", frames: []string{`{"candidates":[{"content":{"parts":[]},"finishReason":"STOP"}]}`}},
		{name: "claude without an end marker", dialect: "claude", usage: `{"type":"message_start","message":{"usage":{"input_tokens":5,"output_tokens":0}}}`, outcome: "ignored", reason: "no_visible_output",
			frames: []string{`{"type":"message_start","message":{"role":"assistant","content":[]}}`, `{"type":"ping"}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			info := newInfo()
			if tc.status != nil {
				tc.status(info.StreamStatus)
			}
			RequestPolicy(c).BeginAttempt(&model.Channel{Id: 11})
			MarkRouteAttemptSent(c, info)
			if tc.usage != "" {
				ObserveRouteUsage(c, tc.dialect, []byte(tc.usage))
			}
			for _, frame := range tc.frames {
				ObserveRouteStreamOutput(c, frame)
			}
			FinishRouteAttempt(c, info, nil)
			events := RequestPolicy(c).Events()
			ev := events[len(events)-1].RouteAttempt
			require.NotNil(t, ev)
			require.Equal(t, tc.outcome, ev.Outcome)
			require.Equal(t, tc.reason, ev.OutcomeReason)
			failures := 0
			if tc.outcome == "failure" {
				failures = 1
			}
			require.Equal(t, failures, routeObservations.breakers[key].failures)
		})
	}
}
