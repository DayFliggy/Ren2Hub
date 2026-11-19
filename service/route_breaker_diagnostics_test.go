package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func routeBreakerRow(t *testing.T, id int, modelName string) (RouteBreakerSnapshot, bool) {
	t.Helper()
	for _, row := range RoutingSnapshot().Breakers {
		if row.ChannelID == id && row.Model == modelName {
			return row, true
		}
	}
	return RouteBreakerSnapshot{}, false
}

// The Root-only routing snapshot explains why a breaker is open and how a
// cooldown started, without any key, session identifier or upstream message.
func TestRouteBreakerSnapshotExplainsTrigger(t *testing.T) {
	info := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, StartTime: time.Now()}
	badGateway := `{"error":{"message":"bad gateway from provider","code":"server_error"}}`

	t.Run("consecutive failures", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		for range routeBreakerOpenFailures {
			finishRouteAttempt(t, c, info, upstreamError(t, http.StatusBadGateway, badGateway, false))
		}
		row, open := routeBreakerRow(t, 11, "route-model")
		require.True(t, open)
		require.Equal(t, "consecutive", row.Trigger)
		require.Equal(t, http.StatusBadGateway, row.LastStatus)
		require.Equal(t, "server_error", row.LastErrorCode)
		require.NotZero(t, row.OpenedAt)
		require.Zero(t, row.Reopens)
		encoded, err := common.Marshal(row)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "bad gateway from provider", "no upstream message")
	})

	// The error code comes from the upstream; only a short identifier is kept.
	// The error code comes from the upstream and may hold anything, also a
	// short credential or identifier. Only codes of the routing error code
	// table are kept; the client still receives the original error.
	t.Run("only known codes are stored", func(t *testing.T) {
		secrets := []string{"sk-test-123", "abc123DEF456ghi789", "123e4567-e89b-12d3-a456-426614174000"}
		cases := []struct{ name, code, want string }{
			{"known upstream code", "server_error", "server_error"},
			{"known local code", string(types.ErrorCodeBadResponseStatusCode), string(types.ErrorCodeBadResponseStatusCode)},
			{"sentence", "Your key sk-test-123 is invalid", routeUnrecognizedErrorCode},
			{"overlong", strings.Repeat("a", 65), routeUnrecognizedErrorCode},
		}
		for _, secret := range secrets {
			cases = append(cases, struct{ name, code, want string }{"short value " + secret, secret, routeUnrecognizedErrorCode})
		}
		for _, tc := range cases {
			t.Run("http "+tc.name, func(t *testing.T) {
				c := routeOutcomeTestContext(t)
				body, err := common.Marshal(map[string]any{"error": map[string]any{"message": "m", "code": tc.code}})
				require.NoError(t, err)
				for range routeBreakerOpenFailures {
					finishRouteAttempt(t, c, info, upstreamError(t, http.StatusBadGateway, string(body), false))
				}
				row, open := routeBreakerRow(t, 11, "route-model")
				require.True(t, open)
				require.Equal(t, tc.want, row.LastErrorCode)
			})
			t.Run("sse "+tc.name, func(t *testing.T) {
				c := routeOutcomeTestContext(t)
				for range routeBreakerOpenFailures {
					status := relaycommon.NewStreamStatus()
					status.RecordError("upstream failed")
					status.MarkFailed(tc.code, "server_error", http.StatusInternalServerError)
					streamInfo := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, IsStream: true, StartTime: time.Now(), StreamStatus: status}
					finishRouteAttempt(t, c, streamInfo, nil)
				}
				row, open := routeBreakerRow(t, 11, "route-model")
				require.True(t, open)
				require.Equal(t, tc.want, row.LastErrorCode)
			})
		}
		t.Run("object code", func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			for range routeBreakerOpenFailures {
				finishRouteAttempt(t, c, info, upstreamError(t, http.StatusBadGateway, `{"error":{"message":"m","code":{"account":"acct-42"}}}`, false))
			}
			row, _ := routeBreakerRow(t, 11, "route-model")
			require.Equal(t, routeUnrecognizedErrorCode, row.LastErrorCode)
		})
		t.Run("routing reason", func(t *testing.T) {
			c := routeOutcomeTestContext(t)
			for range routeBreakerOpenFailures {
				status := relaycommon.NewStreamStatus()
				status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				streamInfo := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, IsStream: true, StartTime: time.Now(), StreamStatus: status}
				finishRouteAttempt(t, c, streamInfo, nil, `{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`, `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
			}
			row, open := routeBreakerRow(t, 11, "route-model")
			require.True(t, open)
			require.Equal(t, routeReasonEmptyStream, row.LastErrorCode)
		})
	})

	t.Run("window failure rate", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		for i := range routeBreakerWindowSamples {
			if i%2 == 0 {
				finishRouteAttempt(t, c, info, upstreamError(t, http.StatusServiceUnavailable, `{"error":{"message":"busy","code":"overloaded_error"}}`, false))
			} else {
				finishRouteAttempt(t, c, info, nil)
			}
		}
		row, open := routeBreakerRow(t, 11, "route-model")
		require.True(t, open)
		require.Equal(t, "window_rate", row.Trigger)
		require.Equal(t, http.StatusServiceUnavailable, row.LastStatus)
		require.Equal(t, "overloaded_error", row.LastErrorCode)
	})

	t.Run("failed probe reopens and publication clears", func(t *testing.T) {
		db := setupRouteTest(t)
		resetRouteObservations(t)
		createRouteChannel(t, db, 11, "route-model")
		for range routeBreakerOpenFailures {
			recordBreaker(11, "route-model", false)
		}
		expireBreakerCooldown(t, 11, "route-model")
		c := routeTestContext(t)
		_, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.NotNil(t, currentRouteProbe(c))
		common.SetContextKey(c, constant.ContextKeyChannelId, 11)
		RequestPolicy(c).BeginAttempt(&model.Channel{Id: 11})
		MarkRouteAttemptSent(c, info)
		FinishRouteAttempt(c, info, upstreamError(t, http.StatusBadGateway, badGateway, false))
		row, open := routeBreakerRow(t, 11, "route-model")
		require.True(t, open)
		require.Equal(t, "open", row.State)
		require.Equal(t, "probe_failed", row.Trigger)
		require.Equal(t, 1, row.Reopens)
		require.Equal(t, http.StatusBadGateway, row.LastStatus)

		require.NoError(t, RefreshChannelConcurrencyLimit(context.Background(), 11))
		_, open = routeBreakerRow(t, 11, "route-model")
		require.False(t, open, "configuration publication leaves no trigger behind")
	})

	t.Run("cooldown source", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		MarkRouteAttemptSent(c, info)
		ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"30"}}})
		FinishRouteAttempt(c, info, nil)
		other := routeTestContext(t)
		BeginRouteObservation(other)
		common.SetContextKey(other, constant.ContextKeyChannelId, 12)
		MarkRouteAttemptSent(other, info)
		ObserveRouteResponse(other, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}})
		FinishRouteAttempt(other, info, nil)
		sources := map[int]string{}
		for _, row := range RoutingSnapshot().Cooldowns {
			sources[row.ChannelID] = row.Source
		}
		require.Equal(t, map[int]string{11: "retry_after", 12: "headerless"}, sources)
	})
}
