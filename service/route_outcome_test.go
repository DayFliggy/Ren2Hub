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
