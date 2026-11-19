package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// Reasons why the routing outcome of an attempt differs from the native
// classification. Only routing health, quality and observations read them;
// retries, the native performance metrics and the client response do not.
const (
	routeReasonContextOverflow  = "request_context_overflow"
	routeReasonEmptyStream      = "empty_stream"
	routeReasonNoVisibleOutput  = "no_visible_output"
	routeReasonRequestTerminate = "request_terminated"
)

// routeOutcome is the single place that turns the native classification into
// the routing outcome of one attempt.
func (a *routeAttempt) routeOutcome(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError) (perfmetrics.Outcome, string) {
	if c.Request.Context().Err() == context.DeadlineExceeded {
		return perfmetrics.OutcomeIgnored, ""
	}
	outcome := perfmetrics.ClassifyRelayOutcome(c.Request.Context(), info, apiErr)
	if outcome == perfmetrics.OutcomeFailure && a.requestContextOverflow(apiErr) {
		return perfmetrics.OutcomeIgnored, routeReasonContextOverflow
	}
	// A realtime connection is not one generation: connecting and closing
	// without asking for a response is not an empty stream.
	if outcome == perfmetrics.OutcomeSuccess && info.IsStream && info.RelayMode != relayconstant.RelayModeRealtime &&
		a.firstOutput.Load() == 0 && a.streamEvents.Load() > 0 {
		return a.emptyStreamOutcome()
	}
	return outcome, ""
}

// emptyStreamOutcome judges a stream that ended natively successful after
// observed events without one content event. It fails the channel only when
// the upstream marked a normal completion and reported no output tokens;
// otherwise the content check may have missed a payload kind, and a wrong
// failure would open a healthy channel's breaker, so the attempt is neither
// punished nor credited. Streams whose handler never reports events keep the
// native result.
func (a *routeAttempt) emptyStreamOutcome() (perfmetrics.Outcome, string) {
	a.usageMu.Lock()
	u := a.usage
	a.usageMu.Unlock()
	switch {
	case u.output.value > 0 || u.thinking.value > 0:
		return perfmetrics.OutcomeIgnored, routeReasonNoVisibleOutput
	case a.requestEnded.Load():
		return perfmetrics.OutcomeIgnored, routeReasonRequestTerminate
	case a.completed.Load():
		return perfmetrics.OutcomeFailure, routeReasonEmptyStream
	}
	return perfmetrics.OutcomeIgnored, routeReasonNoVisibleOutput
}

// requestContextOverflow recognizes a context overflow that the upstream
// reported without a recognizable code, for example a reseller's code-less
// 500. Account and rate-limit statuses are never overruled by message text,
// and only the error object's message is read, never the raw response body,
// so request content echoed elsewhere cannot match.
func (a *routeAttempt) requestContextOverflow(apiErr *types.NewAPIError) bool {
	if apiErr == nil || RelayErrorSource(apiErr) != "upstream" {
		return false
	}
	root := rootRelayError(apiErr)
	for _, status := range []int{root.StatusCode, int(a.status.Load())} {
		switch status {
		case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden, http.StatusTooManyRequests:
			return false
		}
	}
	return contextOverflowMessage(upstreamErrorMessage(root))
}

func rootRelayError(apiErr *types.NewAPIError) *types.NewAPIError {
	for {
		var inner *types.NewAPIError
		if !errors.As(apiErr.Unwrap(), &inner) || inner == apiErr {
			return apiErr
		}
		apiErr = inner
	}
}

// upstreamErrorMessage returns the message field of the upstream error object.
// The wrapped Go error is not used: with show-body enabled it embeds the whole
// response body.
func upstreamErrorMessage(apiErr *types.NewAPIError) string {
	switch relayErr := apiErr.RelayError.(type) {
	case types.OpenAIError:
		return relayErr.Message
	case types.ClaudeError:
		return relayErr.Message
	}
	return ""
}

// contextOverflowMessage holds the overflow phrases of OpenAI-compatible,
// Anthropic and Gemini upstreams in one table.
func contextOverflowMessage(message string) bool {
	text := strings.ToLower(message)
	if text == "" {
		return false
	}
	for _, phrase := range []string{"context_length_exceeded", "context_too_large", "maximum context length", "max context length", "prompt is too long"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	exceeded := strings.Contains(text, "exceed") || strings.Contains(text, "too large") || strings.Contains(text, "too long")
	if exceeded && (strings.Contains(text, "context window") || strings.Contains(text, "context length")) {
		return true
	}
	return strings.Contains(text, "input token count") && strings.Contains(text, "exceeds the maximum")
}

// routeFailureCode names a failed attempt in the breaker snapshot: the root
// error code, the stream's terminal error code, or the routing reason such as
// empty_stream. It never carries the upstream message.
func routeFailureCode(info *relaycommon.RelayInfo, apiErr *types.NewAPIError, reason string) string {
	if apiErr != nil {
		return string(rootRelayError(apiErr).GetErrorCode())
	}
	if code := info.StreamStatus.OutcomeSnapshot().ErrorCode; code != "" {
		return code
	}
	return reason
}
