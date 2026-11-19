package service

import (
	"context"
	"errors"
	"net/http"
	"regexp"
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
// the handler gave the raw upstream frames to ObserveRouteUsage, the upstream
// marked a normal completion and the usage reports no output. A handler that
// shows only converted frames (Cohere, PaLM, Xunfei, Zhipu, image, TTS) may
// hide billed output, a usage conflict leaves the output count unknown, and
// without a completion marker the content check may have missed a payload
// kind; a wrong failure would open a healthy channel's breaker, so these are
// neither punished nor credited. Streams whose handler never reports events
// keep the native result.
func (a *routeAttempt) emptyStreamOutcome() (perfmetrics.Outcome, string) {
	a.usageMu.Lock()
	u := a.usage
	a.usageMu.Unlock()
	switch {
	case u.conflict || u.output.value > 0 || u.thinking.value > 0:
		return perfmetrics.OutcomeIgnored, routeReasonNoVisibleOutput
	case a.requestEnded.Load():
		return perfmetrics.OutcomeIgnored, routeReasonRequestTerminate
	case a.completed.Load() && a.rawObserved.Load():
		return perfmetrics.OutcomeFailure, routeReasonEmptyStream
	}
	return perfmetrics.OutcomeIgnored, routeReasonNoVisibleOutput
}

// requestContextOverflow recognizes a context overflow that the upstream
// reported without a recognizable code, for example a reseller's code-less
// 500. Account and rate-limit statuses, codes and types are never overruled
// by message text.
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
	for _, signal := range []string{string(root.GetErrorCode()), root.ToOpenAIError().Type} {
		if class, _ := routeErrorCodeClass(signal); class == routeErrorAccount {
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

// upstreamErrorMessage returns the normalized upstream error message. It is
// the error object's message, or, when the body had no error object, the
// message the native handler took from top-level fields such as detail
// (service/error.go); OpenRouter metadata is already appended to it
// (relaykit/types/error.go). All of it is upstream error text. The wrapped Go
// error is not used: with show-body enabled it embeds the whole response body.
func upstreamErrorMessage(apiErr *types.NewAPIError) string {
	switch relayErr := apiErr.RelayError.(type) {
	case types.OpenAIError:
		return relayErr.Message
	case types.ClaudeError:
		return relayErr.Message
	}
	return ""
}

// quotedText matches double-quoted, curly-quoted and backtick spans: request
// content an upstream echoes back. Single quotes are kept because they are
// also apostrophes ("This model's maximum context length").
var quotedText = regexp.MustCompile("\"[^\"]*\"|“[^”]*”|`[^`]*`")

// contextLimitReport is the canonical limit report of OpenAI-compatible
// upstreams, "This model's maximum context length is 128000 tokens": stating
// the limit as a number is itself the overflow report.
var contextLimitReport = regexp.MustCompile(`\bmax(imum)? context length is \d`)

// contextOverflowMessage holds the overflow phrases of OpenAI-compatible,
// Anthropic and Gemini upstreams in one table. Error codes, complete overflow
// sentences and the numeric limit report match alone; a phrase that only
// names the limit, such as "maximum context length", also needs overflow
// wording, so a fault that merely mentions the setting does not match.
func contextOverflowMessage(message string) bool {
	text := quotedText.ReplaceAllString(strings.ToLower(message), "")
	if text == "" {
		return false
	}
	for _, phrase := range []string{"context_length_exceeded", "context_too_large", "prompt is too long"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	if strings.Contains(text, "input token count") && strings.Contains(text, "exceeds the maximum") {
		return true
	}
	if contextLimitReport.MatchString(text) {
		return true
	}
	limit := false
	for _, phrase := range []string{"maximum context length", "max context length", "context window", "context length"} {
		limit = limit || strings.Contains(text, phrase)
	}
	if !limit {
		return false
	}
	for _, phrase := range []string{"exceed", "too large", "too long", "reached", "resulted in", "you requested", "reduce the length", "greater than", "more than"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
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
