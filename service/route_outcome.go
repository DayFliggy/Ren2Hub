package service

import (
	"context"
	"errors"
	"net/http"
	"strings"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// Reasons why the routing outcome of an attempt differs from the native
// classification. Only routing health, quality and observations read them;
// retries, the native performance metrics and the client response do not.
const routeReasonContextOverflow = "request_context_overflow"

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
	return outcome, ""
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
