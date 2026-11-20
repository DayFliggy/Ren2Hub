package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteQuotaFailureOpensBreaker(t *testing.T) {
	for _, tc := range []struct {
		name           string
		err            *types.NewAPIError
		qualityFailure bool
		opens          bool
	}{
		{"anthropic credit", types.WithClaudeError(types.ClaudeError{Type: "invalid_request_error", Message: "Your credit balance is too low"}, 400), true, true},
		{"reseller quota", types.WithOpenAIError(types.OpenAIError{Code: "pre_consume_token_quota_failed"}, 403), true, true},
		{"quota 429", types.WithOpenAIError(types.OpenAIError{Code: "insufficient_quota"}, 429), true, true},
		{"user quota", types.WithOpenAIError(types.OpenAIError{Code: "insufficient_user_quota"}, 429), true, true},
		{"quota text", types.WithOpenAIError(types.OpenAIError{Message: "You exceeded your current quota"}, 429), true, false},
		{"echo400", types.WithOpenAIError(types.OpenAIError{Code: "invalid_request_error", Type: "invalid_request_error", Message: `Invalid argument: "Your credit balance is too low" is not a valid model`}, 400), false, false},
		{"context400", types.WithOpenAIError(types.OpenAIError{Code: "context_length_exceeded", Type: "invalid_request_error", Message: `Maximum context length exceeded for input "You exceeded your current quota"`}, 400), false, false},
		{"rate429", types.WithOpenAIError(types.OpenAIError{Code: "rate_limit_exceeded", Type: "invalid_request_error", Message: `Rate limited for model "You exceeded your current quota"`}, 429), true, false},
		{"explicit context code", types.WithOpenAIError(types.OpenAIError{Code: "context_length_exceeded", Message: "Your credit balance is too low"}, 400), false, false},
		{"quoted prefix", types.WithClaudeError(types.ClaudeError{Type: "invalid_request_error", Message: `“Your credit balance is too low” is not a valid model`}, 400), false, false},
		{"trimmed prefix", types.WithClaudeError(types.ClaudeError{Type: "invalid_request_error", Message: "  Your credit balance is too low to access the API."}, 400), true, true},
		{"credit text 429", types.WithClaudeError(types.ClaudeError{Type: "rate_limit_error", Message: "Your credit balance is too low"}, 429), true, false},
		{"gemini per-minute", upstreamError(t, 429, geminiPerMinuteResponse, false), true, false},
		{"ordinary 400", types.WithOpenAIError(types.OpenAIError{Type: "invalid_request_error"}, 400), false, false},
		{"ordinary 429", types.WithOpenAIError(types.OpenAIError{Code: "rate_limit_exceeded"}, 429), true, false},
		{"local quota", types.NewError(errors.New("You exceeded your current quota"), types.ErrorCodeInsufficientUserQuota), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			info := &relaycommon.RelayInfo{OriginModelName: "quota-model", RelayMode: 1, StartTime: time.Now()}
			for range routeBreakerOpenFailures {
				MarkRouteAttemptSent(c, info)
				before := time.Now()
				ObserveRouteResponse(c, &http.Response{StatusCode: tc.err.StatusCode, Header: http.Header{}})
				after := time.Now()
				FinishRouteAttempt(c, info, tc.err)
				if tc.err.StatusCode == 429 {
					cooldown := routeObservations.cooldowns[11]
					require.Equal(t, "headerless", cooldown.source)
					require.GreaterOrEqual(t, cooldown.until, before.Add(5*time.Second).UnixMilli())
					require.LessOrEqual(t, cooldown.until, after.Add(5*time.Second).UnixMilli())
				} else {
					require.Empty(t, routeObservations.cooldowns)
				}
			}
			require.Equal(t, tc.opens, routeObservations.breakers[routeBreakerKey{11, info.OriginModelName}].openUntil > 0)
			total, good := routeQualityTotals(11, info.OriginModelName)
			if tc.qualityFailure {
				require.EqualValues(t, routeBreakerOpenFailures, total)
			} else {
				require.Zero(t, total)
			}
			require.Zero(t, good)
		})
	}
}

const geminiPerMinuteResponse = `{"error":{"code":429,"message":"You exceeded your current quota, please check your plan and billing details. For more information on this error, head to: https://ai.google.dev/gemini-api/docs/rate-limits.","status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.QuotaFailure","violations":[{"quotaMetric":"generativelanguage.googleapis.com/generate_content_free_tier_requests","quotaId":"GenerateRequestsPerMinutePerProjectPerModel-FreeTier"}]},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"21s"}]}}`

// C1c supplied normalized provider errors directly to FinishRouteAttempt.
func TestRouteQuotaExperimentResponses(t *testing.T) {
	c := routeOutcomeTestContext(t)
	info := &relaycommon.RelayInfo{OriginModelName: "quota-model", RelayMode: 1, StartTime: time.Now()}
	for _, tc := range []struct {
		name            string
		err             *types.NewAPIError
		outcome, reason string
	}{
		{"credit", types.WithClaudeError(types.ClaudeError{Type: "invalid_request_error", Message: "Your credit balance is too low"}, 400), "failure", "upstream_quota"},
		{"reseller", types.WithOpenAIError(types.OpenAIError{Code: "pre_consume_token_quota_failed"}, 403), "failure", "upstream_quota"},
		{"quota", types.WithOpenAIError(types.OpenAIError{Code: "insufficient_quota", Message: "You exceeded your current quota"}, 429), "failure", "upstream_quota"},
		{"rate", types.WithOpenAIError(types.OpenAIError{Code: "rate_limit_exceeded"}, 429), "failure", ""},
		{"invalid", types.WithOpenAIError(types.OpenAIError{Type: "invalid_request_error"}, 400), "ignored", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := finishRouteAttempt(t, c, info, tc.err)
			require.Equal(t, tc.outcome, ev.Outcome)
			require.Equal(t, tc.reason, ev.OutcomeReason)
		})
	}
}

func TestRouteQuotaIgnoredAfterStreamOutput(t *testing.T) {
	c := routeOutcomeTestContext(t)
	info := &relaycommon.RelayInfo{OriginModelName: "quota-model", RelayMode: 1, IsStream: true, StartTime: time.Now()}
	err := types.WithClaudeError(types.ClaudeError{Type: "invalid_request_error", Message: "Your credit balance is too low"}, 400)
	for range routeBreakerOpenFailures {
		ev := finishRouteAttempt(t, c, info, err, `{"choices":[{"delta":{"content":"hi"}}]}`)
		require.Equal(t, "ignored", ev.Outcome)
		require.Empty(t, ev.OutcomeReason)
	}
	require.Zero(t, routeObservations.breakers[routeBreakerKey{11, info.OriginModelName}].openUntil)
	require.Zero(t, routeQualitySampleCount(11))
}
func TestRouteQuotaIgnoresCancelledAndBusinessRejections(t *testing.T) {
	for _, kind := range []string{"cancelled", "business"} {
		t.Run(kind, func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			info := &relaycommon.RelayInfo{OriginModelName: "quota-model", RelayMode: 1, PerformanceBusinessRejection: kind == "business", StartTime: time.Now()}
			if kind == "cancelled" {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			err := types.WithOpenAIError(types.OpenAIError{Code: "insufficient_quota"}, 429)
			for range routeBreakerOpenFailures {
				ev := finishRouteAttempt(t, c, info, err)
				require.Equal(t, "ignored", ev.Outcome)
				require.Empty(t, ev.OutcomeReason)
			}
			require.Zero(t, routeObservations.breakers[routeBreakerKey{11, info.OriginModelName}].openUntil)
			require.Zero(t, routeQualitySampleCount(11))
		})
	}
}

func TestRouteQuotaIgnoresLocalBillingRejections(t *testing.T) {
	setupCatalogRouteTest(t)
	for _, kind := range []string{"user", "token", "wallet"} {
		t.Run(kind, func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Token{}))
			u := model.User{Username: "quota-test-user", Quota: 0}
			require.NoError(t, model.DB.Create(&u).Error)
			token := model.Token{UserId: u.Id, Key: "quota-test-key", RemainQuota: 0}
			require.NoError(t, model.DB.Create(&token).Error)
			info := &relaycommon.RelayInfo{UserId: u.Id, TokenId: token.Id, TokenKey: token.Key, OriginModelName: "quota-model", RelayMode: 1, StartTime: time.Now()}
			for range routeBreakerOpenFailures {
				var e *types.NewAPIError
				switch kind {
				case "user":
					_, e = NewBillingSession(c, info, 1)
				case "token":
					s := BillingSession{relayInfo: info, funding: &WalletFunding{userId: u.Id}}
					e = s.preConsume(c, 1)
				case "wallet":
					err := (&BillingSession{relayInfo: info, funding: &WalletFunding{userId: u.Id}}).reserveFunding(1)
					require.ErrorAs(t, err, &e)
				}
				require.NotNil(t, e)
				require.Equal(t, "local", RelayErrorSource(e))
				require.False(t, (&routeAttempt{}).upstreamQuotaFailure(e))
				// Even force an already-sent attempt: classification alone must protect the channel.
				MarkRouteAttemptSent(c, info)
				FinishRouteAttempt(c, info, e)
			}
			require.Zero(t, routeObservations.breakers[routeBreakerKey{11, info.OriginModelName}].openUntil)
			require.Zero(t, routeQualitySampleCount(11))
		})
	}
}

func TestRouteQuotaIgnoresCreditTextOnRemapped429(t *testing.T) {
	checkCreditTextOnObserved429(t, http.StatusBadRequest)
}

func TestRouteQuotaIgnoresCreditTextOnUnmapped429(t *testing.T) {
	checkCreditTextOnObserved429(t, http.StatusTooManyRequests)
}

func checkCreditTextOnObserved429(t *testing.T, relayStatus int) {
	t.Helper()
	c := routeOutcomeTestContext(t)
	ch := &model.Channel{Id: 11}
	info := &relaycommon.RelayInfo{OriginModelName: "quota-model", RelayMode: 1, StartTime: time.Now()}
	apiErr := types.WithClaudeError(types.ClaudeError{
		Type: "invalid_request_error", Message: "Your credit balance is too low to access the API.",
	}, relayStatus)

	for range routeBreakerOpenFailures {
		RequestPolicy(c).BeginAttempt(ch)
		MarkRouteAttemptSent(c, info)
		ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests})
		FinishRouteAttempt(c, info, apiErr)
	}

	attempts := 0
	for _, event := range RequestPolicy(c).Events() {
		if event.RouteAttempt != nil {
			attempts++
			assert.Empty(t, event.RouteAttempt.OutcomeReason, "HTTP 429 with only credit text is not a quota signal")
		}
	}
	require.EqualValues(t, routeBreakerOpenFailures, attempts)
	require.Equal(t, "headerless", routeObservations.cooldowns[ch.Id].source)
	require.Zero(t, routeObservations.breakers[routeBreakerKey{ch.Id, info.OriginModelName}].openUntil)
}
