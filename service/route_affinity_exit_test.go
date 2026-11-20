package service

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// isolateRouteQualities gives the test an empty quality store.
func isolateRouteQualities(t *testing.T) {
	t.Helper()
	previous := routeQualities
	routeQualities = newRouteQualityStore()
	t.Cleanup(func() { routeQualities = previous })
}

// rateChannels publishes quality bands for the test's chat requests from
// first-output latencies: 22 at boundMS, 11 at 100 ms.
func rateChannels(t *testing.T, boundMS int64) {
	t.Helper()
	now := time.Now()
	for range 25 {
		for id, latency := range map[int]int64{11: 100, 22: boundMS} {
			routeQualities.add(routeQualityKey{"route-model", routeObservationKey{id, "route-model", relayconstant.RelayModeChatCompletions, false}}, true, latency, 0, 0, now)
		}
	}
	routeQualities.evaluate(now)
}

func TestPreferAffinityLeavesSeverelyDowngradedChannel(t *testing.T) {
	t.Run("band 2 leaves for a better channel and the success rebinds", func(t *testing.T) {
		setupAffinityChannels(t)
		isolateRouteQualities(t)
		rateChannels(t, 20000)
		c, key := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 11, selected.Id)
		require.Equal(t, "affinity_exit", c.GetString("route_admission_source"))
		info, _ := c.Get(ginKeyChannelAffinityLogInfo)
		require.Nil(t, info, "the binding did not choose this channel")
		bound, found := affinityBinding(t, key)
		require.True(t, found, "leaving for one request keeps the binding")
		require.Equal(t, 22, bound)

		c.Set("channel_id", selected.Id)
		RecordChannelAffinity(c, selected.Id)
		bound, _ = affinityBinding(t, key)
		require.Equal(t, 11, bound, "SwitchOnSuccess rebinds to the channel that served")
	})
	t.Run("with switch_on_success off the success keeps the binding", func(t *testing.T) {
		setupAffinityChannels(t)
		isolateRouteQualities(t)
		rateChannels(t, 20000)
		c, key := enableRouteAffinity(t, "prefer", 22)
		operation_setting.GetChannelAffinitySetting().SwitchOnSuccess = false
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 11, selected.Id)

		// As middleware.Distribute records a success.
		c.Set("channel_id", selected.Id)
		RecordChannelAffinity(c, selected.Id)
		bound, found := affinityBinding(t, key)
		require.True(t, found)
		require.Equal(t, 22, bound, "the request left the binding without moving it")
	})
	t.Run("band 1 keeps the bound channel", func(t *testing.T) {
		setupAffinityChannels(t)
		isolateRouteQualities(t)
		rateChannels(t, 1500)
		c, _ := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		require.Equal(t, "prefer", c.GetString("route_admission_source"))
	})
	t.Run("a full better channel keeps the bound channel", func(t *testing.T) {
		db := setupAffinityChannels(t)
		isolateRouteQualities(t)
		rateChannels(t, 20000)
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("max_concurrency", 1).Error)
		holder, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11, MaxConcurrency: 1})
		require.NoError(t, err)
		defer holder.Release()
		c, _ := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		require.Equal(t, 1, ChannelInFlight(11), "the full channel got no second permit")
	})
	t.Run("a better channel with an open breaker keeps the bound channel", func(t *testing.T) {
		setupAffinityChannels(t)
		resetRouteObservations(t)
		isolateRouteQualities(t)
		rateChannels(t, 20000)
		routeObservations.breakers[routeBreakerKey{11, "route-model"}] = routeBreakerState{openUntil: time.Now().Add(time.Minute).UnixMilli()}
		c, _ := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
	})
	t.Run("strict affinity ignores the downgrade", func(t *testing.T) {
		setupAffinityChannels(t)
		isolateRouteQualities(t)
		rateChannels(t, 20000)
		c, _ := enableRouteAffinity(t, "strict", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
	})
	t.Run("plugin reacquisition of the downgraded binding selects again", func(t *testing.T) {
		setupAffinityChannels(t)
		isolateRouteQualities(t)
		c, _ := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		ReleaseRouteChannel(c)
		// The binding degrades between preselection and reacquisition.
		rateChannels(t, 20000)

		reacquired, selectErr := ReacquireSelectedRouteChannel(c, 22, "route-model")
		require.Nil(t, selectErr)
		require.Nil(t, reacquired, "the caller selects again rather than taking another channel silently")
		require.Zero(t, ChannelInFlight(22))
		require.Zero(t, ChannelInFlight(11))
		info, _ := c.Get(ginKeyChannelAffinityLogInfo)
		require.Nil(t, info)
		selected, selectErr = SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 11, selected.Id)
	})
}

// A session bound to 22 sends one request every 10 seconds while other users'
// requests reach 11 through normal selection. 22 slows from 1 to 20 seconds;
// the real evaluator, running on its own ticker, has to rate it from the
// session's own samples before the session leaves it.
func TestPreferAffinityWorkloadLeavesSlowBinding(t *testing.T) {
	setupAffinityChannels(t)
	resetRouteObservations(t)
	isolateRouteQualities(t)
	synctest.Test(t, func(t *testing.T) {
		stop := StartRoutingEvaluation()
		defer stop()
		session, key := enableRouteAffinity(t, "prefer", 22)
		header := session.Request.Header.Get("X-Session")
		send := func(n int, bound bool) int {
			c := routeTestContext(t)
			c.Set(common.RequestIdKey, fmt.Sprintf("affinity-workload-%04d", n))
			if bound {
				c.Request.Header.Set("X-Session", header)
			}
			info := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: relayconstant.RelayModeChatCompletions, StartTime: time.Now()}
			channel, selectErr := SelectChannelForRequest(c, "route-model", &RetryParam{Retry: common.GetPointer(0)})
			require.Nil(t, selectErr)
			common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
			c.Set("channel_id", channel.Id)
			MarkRouteAttemptSent(c, info)
			latency := time.Second
			if channel.Id == 22 && n >= 60 {
				latency = 20 * time.Second
			}
			time.Sleep(latency)
			FinishRouteAttempt(c, info, nil)
			if bound {
				RecordChannelAffinity(c, channel.Id)
			}
			return channel.Id
		}
		var served []int
		for n := range 360 {
			if n%2 == 0 {
				served = append(served, send(n, true))
			} else {
				send(n, false)
			}
			time.Sleep(10 * time.Second)
		}
		left := -1
		for i, id := range served {
			if id == 11 {
				left = i
				break
			}
		}
		t.Logf("session requests=%d left at %d", len(served), left)
		require.Positive(t, left, "the session starts on its binding and leaves it later")
		for _, id := range served[left:] {
			require.Equal(t, 11, id, "after leaving, the session stays on the rebinding")
		}
		bound, found := affinityBinding(t, key)
		require.True(t, found)
		require.Equal(t, 11, bound)
		snapshot := routeQualities.snapshot()[routeQualityKey{"route-model", routeObservationKey{22, "route-model", relayconstant.RelayModeChatCompletions, false}}]
		require.Equal(t, 2, snapshot.Band, "the evaluator, not the test, rated the binding")
	})
}

// A yielding request ranks the better candidates as automatic selection does
// for the same request, cache bands included: 11 reuses this session's cache
// poorly, so the request leaves 22 for 33.
func TestPreferAffinityExitRanksWithTheRequestCache(t *testing.T) {
	db := setupAffinityChannels(t)
	createRouteChannel(t, db, 33, "route-model")
	isolateRouteQualities(t)
	rateChannels(t, 20000)
	previousCaches := routeCaches
	routeCaches = newCacheStore()
	t.Cleanup(func() { routeCaches = previousCaches })
	c, _ := enableRouteAffinity(t, "prefer", 22)
	request := cacheTestRequest(0)
	c.Set("route_cache_request", request)
	operation := routeOperation(c)
	now := time.Now()
	start := now.Truncate(cachePeriod).Add(-3 * cachePeriod)
	for batch := range 2 {
		at := start.Add(time.Duration(2*batch)*cachePeriod + time.Minute)
		for session := range 20 {
			for round := range 5 {
				tm := at.Add(time.Duration(round) * time.Second)
				for id, read := range map[int]int64{11: 20, 33: 80} {
					routeCaches.record(cacheTestRequest(session+25*batch), "route-model", routeObservationKey{id, "route-model", operation, false}, tm, tm.Add(time.Millisecond), read, 100, true, "auto")
				}
			}
		}
		routeCaches.evaluate(start.Add(time.Duration(1+2*batch) * cachePeriod))
		if batch == 0 {
			routeCaches.evaluate(start.Add(2 * cachePeriod))
		}
	}
	bands := routeCaches.bands(request, "route-model", operation, false, now)
	require.Equal(t, 2, bands[11].Band)
	require.True(t, bands[33].Known)

	// Automatic selection of the same request, without exploration.
	reference := routeTestContext(t)
	reference.Set("route_cache_request", request)
	reference.Set("use_channel", []string{"reference"})
	rows := make([]*model.Channel, 0, 2)
	for _, id := range []int{11, 33} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		rows = append(rows, channel)
	}
	orderRuntimeRoutes(reference, rows, "route-model", operation, false)
	require.Equal(t, 33, rows[0].Id)

	selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 33, selected.Id)
	require.Equal(t, "affinity_exit", c.GetString("route_admission_source"))
}
