package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
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
		{"quota text", types.WithOpenAIError(types.OpenAIError{Message: "You exceeded your current quota"}, 429), true, true},
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
