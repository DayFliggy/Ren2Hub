package relay

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func TestNativeCountUpstreamErrorHealthOutcome(t *testing.T) {
	tests := []struct {
		name     string
		provider int
		status   int
		body     string
		want     perfmetrics.Outcome
	}{
		{"Claude invalid input", constant.ChannelTypeAnthropic, 400, `{"type":"error","error":{"type":"invalid_request_error","message":"messages: invalid image"}}`, perfmetrics.OutcomeIgnored},
		{"Gemini invalid input", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","message":"invalid content"}}`, perfmetrics.OutcomeIgnored},
		{"Gemini field violation", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest","fieldViolations":[{"field":"contents","description":"invalid value"}]}]}}`, perfmetrics.OutcomeIgnored},
		{"Claude credential", constant.ChannelTypeAnthropic, 401, `{"error":{"type":"authentication_error"}}`, perfmetrics.OutcomeFailure},
		{"Claude credential in 400", constant.ChannelTypeAnthropic, 400, `{"error":{"type":"authentication_error"}}`, perfmetrics.OutcomeFailure},
		{"Claude specific code overrides type", constant.ChannelTypeAnthropic, 400, `{"error":{"type":"invalid_request_error","code":"invalid_api_key"}}`, perfmetrics.OutcomeFailure},
		{"Gemini credential in invalid argument", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`, perfmetrics.OutcomeFailure},
		{"Gemini other reason", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_EXPIRED"}]}}`, perfmetrics.OutcomeFailure},
		{"Gemini mixed details", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"@type":"type.googleapis.com/google.rpc.BadRequest"},{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"API_KEY_INVALID"}]}}`, perfmetrics.OutcomeFailure},
		{"Gemini unknown details", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":[{"reason":"unclassified"}]}}`, perfmetrics.OutcomeFailure},
		{"Gemini malformed details", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT","details":"bad shape"}}`, perfmetrics.OutcomeFailure},
		{"Gemini quota", constant.ChannelTypeGemini, 400, `{"error":{"code":400,"status":"RESOURCE_EXHAUSTED"}}`, perfmetrics.OutcomeFailure},
		{"Gemini mismatched code", constant.ChannelTypeGemini, 400, `{"error":{"code":403,"status":"INVALID_ARGUMENT"}}`, perfmetrics.OutcomeFailure},
		{"Gemini missing code", constant.ChannelTypeGemini, 400, `{"error":{"status":"INVALID_ARGUMENT"}}`, perfmetrics.OutcomeFailure},
		{"Gemini wrong protocol", constant.ChannelTypeGemini, 400, `{"error":{"type":"invalid_request_error"}}`, perfmetrics.OutcomeFailure},
		{"Claude wrong protocol", constant.ChannelTypeAnthropic, 400, `{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`, perfmetrics.OutcomeFailure},
		{"untyped 400", constant.ChannelTypeGemini, 400, `{"error":{"message":"bad credential or prompt"}}`, perfmetrics.OutcomeFailure},
		{"unknown 400", constant.ChannelTypeAnthropic, 400, `{"error":{"type":"new_provider_error"}}`, perfmetrics.OutcomeFailure},
		{"malformed error", constant.ChannelTypeAnthropic, 400, `{"error":`, perfmetrics.OutcomeFailure},
		{"forbidden with misleading type", constant.ChannelTypeAnthropic, 403, `{"error":{"type":"invalid_request_error"}}`, perfmetrics.OutcomeFailure},
		{"limited with misleading type", constant.ChannelTypeGemini, 429, `{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`, perfmetrics.OutcomeFailure},
		{"server with misleading type", constant.ChannelTypeAnthropic, 503, `{"error":{"type":"invalid_request_error"}}`, perfmetrics.OutcomeFailure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := nativeCountUpstreamError(tc.provider, tc.status, []byte(tc.body))
			if apiErr.StatusCode != tc.status || !types.IsSkipRetryError(apiErr) {
				t.Fatalf("classification changed status/retry policy: %#v", apiErr)
			}
			// Route attempt completion uses this classifier to decide whether
			// an error contributes a channel/model breaker failure.
			outcome := perfmetrics.ClassifyRelayOutcome(context.Background(), &relaycommon.RelayInfo{}, apiErr)
			if outcome != tc.want {
				t.Fatalf("health outcome = %s, want %s", outcome, tc.want)
			}
		})
	}
}
