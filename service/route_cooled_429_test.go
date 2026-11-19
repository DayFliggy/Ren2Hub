package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// rateLimitedAttempt selects a channel for modelName, sends one attempt that
// the upstream answers with 429 and the given headers, and finishes it.
func rateLimitedAttempt(t *testing.T, modelName string, header http.Header) (*gin.Context, *ChannelSelectError) {
	t.Helper()
	c := routeTestContext(t)
	ch, selErr := SelectChannelForRequest(c, modelName, nil)
	if selErr != nil {
		return c, selErr
	}
	common.SetContextKey(c, constant.ContextKeyChannelId, ch.Id)
	info := &relaycommon.RelayInfo{OriginModelName: modelName, RelayMode: 1, StartTime: time.Now()}
	MarkRouteAttemptSent(c, info)
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests, Header: header})
	FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests))
	return c, nil
}

func routeQualityTotals(channelID int, modelName string) (total, successes uint32) {
	routeQualities.mu.Lock()
	defer routeQualities.mu.Unlock()
	for key, history := range routeQualities.history {
		if key.id != channelID || key.model != modelName {
			continue
		}
		for _, bucket := range history.buckets {
			total += bucket.total
			successes += bucket.successes
		}
	}
	return total, successes
}

// D3: a 429 that put the channel into a cooldown is not a breaker failure.
// The only channel keeps dispatching while it cools, so the client receives the
// upstream 429 instead of a breaker 503; quality still counts the failure.
func TestCooled429DoesNotOpenTheBreaker(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	cases := map[string]struct {
		channelID int
		header    http.Header
	}{
		"retry-after": {11, http.Header{"Retry-After": []string{"30"}}},
		"headerless":  {22, nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			modelName := "cooled-" + name
			createRouteChannel(t, db, tc.channelID, modelName)
			routeQualities.invalidate(tc.channelID)
			t.Cleanup(func() { routeQualities.invalidate(tc.channelID) })
			for range routeBreakerOpenFailures + 2 {
				c, selErr := rateLimitedAttempt(t, modelName, tc.header)
				require.Nil(t, selErr, "a breaker never opens on cooled 429s")
				require.Equal(t, tc.channelID, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
			}
			key := routeBreakerKey{tc.channelID, modelName}
			require.Zero(t, routeObservations.breakers[key].failures)
			require.Zero(t, routeObservations.breakers[key].openUntil)
			class, _ := routeObservations.availability(key)
			require.Equal(t, routeCooling, class)
			total, successes := routeQualityTotals(tc.channelID, modelName)
			require.EqualValues(t, routeBreakerOpenFailures+2, total, "quality still records every attempt")
			require.Zero(t, successes)
		})
	}
}

// A 429 that wrote no cooldown keeps the existing breaker counting.
func TestUncooled429StillCountsTowardTheBreaker(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "uncooled-model")
	for range routeBreakerOpenFailures {
		_, selErr := rateLimitedAttempt(t, "uncooled-model", http.Header{"Retry-After": []string{"0"}})
		require.Nil(t, selErr)
	}
	require.Empty(t, routeObservations.cooldowns)
	_, selErr := rateLimitedAttempt(t, "uncooled-model", nil)
	require.NotNil(t, selErr)
	require.Equal(t, http.StatusServiceUnavailable, selErr.StatusCode)
	require.Equal(t, "channel_temporarily_unavailable", string(selErr.Code))
}

// A half-open probe answered by a cooled 429 returns its permit: the breaker
// neither advances towards closing nor reopens.
func TestHalfOpenProbeCooled429ReturnsThePermit(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	key := routeBreakerKey{11, "route-model"}
	for range routeBreakerOpenFailures {
		recordBreaker(11, key.model, false)
	}
	expireBreakerCooldown(t, 11, key.model)
	before := routeObservations.breakers[key]

	c := routeTestContext(t)
	ch, selErr := SelectChannelForRequest(c, key.model, nil)
	require.Nil(t, selErr)
	require.NotNil(t, currentRouteProbe(c), "the request holds the probe")
	common.SetContextKey(c, constant.ContextKeyChannelId, ch.Id)
	info := &relaycommon.RelayInfo{OriginModelName: key.model, RelayMode: 1, StartTime: time.Now()}
	MarkRouteAttemptSent(c, info)
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"1"}}})
	FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests))

	after := routeObservations.breakers[key]
	require.Nil(t, after.probe, "the permit is returned")
	require.Equal(t, before.openUntil, after.openUntil, "the breaker does not reopen")
	require.Equal(t, before.reopens, after.reopens)
	require.Zero(t, after.successes, "a 429 is no evidence of recovery")
	require.Zero(t, ChannelInFlight(11))
}
