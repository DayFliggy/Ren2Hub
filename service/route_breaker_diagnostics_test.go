package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
	badGateway := `{"error":{"message":"bad gateway from provider","code":"bad_gateway"}}`

	t.Run("consecutive failures", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		for range routeBreakerOpenFailures {
			finishRouteAttempt(t, c, info, upstreamError(t, http.StatusBadGateway, badGateway, false))
		}
		row, open := routeBreakerRow(t, 11, "route-model")
		require.True(t, open)
		require.Equal(t, "consecutive", row.Trigger)
		require.Equal(t, http.StatusBadGateway, row.LastStatus)
		require.Equal(t, "bad_gateway", row.LastErrorCode)
		require.NotZero(t, row.OpenedAt)
		require.Zero(t, row.Reopens)
		encoded, err := common.Marshal(row)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "bad gateway from provider", "no upstream message")
	})

	t.Run("window failure rate", func(t *testing.T) {
		c := routeOutcomeTestContext(t)
		for i := range routeBreakerWindowSamples {
			if i%2 == 0 {
				finishRouteAttempt(t, c, info, upstreamError(t, http.StatusServiceUnavailable, `{"error":{"message":"busy","code":"overloaded"}}`, false))
			} else {
				finishRouteAttempt(t, c, info, nil)
			}
		}
		row, open := routeBreakerRow(t, 11, "route-model")
		require.True(t, open)
		require.Equal(t, "window_rate", row.Trigger)
		require.Equal(t, http.StatusServiceUnavailable, row.LastStatus)
		require.Equal(t, "overloaded", row.LastErrorCode)
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
