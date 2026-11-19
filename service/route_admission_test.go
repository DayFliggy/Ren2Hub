package service

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRouteBreakerWindowBoundaries(t *testing.T) {
	resetRouteObservations(t)
	synctest.Test(t, func(t *testing.T) {
		for i := range 19 {
			recordBreaker(11, "m", i%2 == 0)
		}
		health, _ := routeObservations.routeHealth(11, "m")
		require.Zero(t, health)
		recordBreaker(11, "m", false)
		class, _ := routeObservations.availability(routeBreakerKey{11, "m"})
		require.Equal(t, routeOpen, class)
		for i := range 19 {
			recordBreaker(12, "m", i%2 == 0)
		}
		time.Sleep(time.Minute)
		recordBreaker(12, "m", false)
		class, _ = routeObservations.availability(routeBreakerKey{12, "m"})
		require.Equal(t, routeAvailable, class)
	})
}

func TestRouteProbeLeaseLifecycle(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	key := routeBreakerKey{11, "route-model"}
	for range routeBreakerOpenFailures {
		recordBreaker(11, key.model, false)
	}
	c := routeTestContext(t)
	_, selErr := SelectChannelForRequest(c, key.model, nil)
	require.Equal(t, http.StatusServiceUnavailable, selErr.StatusCode)
	require.Equal(t, "channel_temporarily_unavailable", string(selErr.Code))
	require.False(t, selErr.NoAvailableChannel)
	require.NotEmpty(t, c.Writer.Header().Get("Retry-After"))
	require.Zero(t, ChannelInFlight(11))
	expireBreakerCooldown(t, 11, key.model)
	ch, selErr := PreselectTaskPluginEndpointChannel(c, key.model)
	require.Nil(t, selErr)
	require.Equal(t, 11, ch.Id)
	require.Nil(t, currentRouteProbe(c), "preselection must not consume the probe")
	ReleaseRouteChannel(c)

	contexts := make([]*gin.Context, 100)
	leases := make([]*routeProbe, 100)
	errs := make([]*ChannelSelectError, 100)
	for i := range contexts {
		contexts[i] = routeTestContext(t)
	}
	start := make(chan struct{})
	var finished sync.WaitGroup
	for i := range contexts {
		finished.Go(func() {
			<-start
			_, errs[i] = SelectChannelForRequest(contexts[i], key.model, nil)
			leases[i] = currentRouteProbe(contexts[i])
		})
	}
	close(start)
	finished.Wait()
	var winner *gin.Context
	for i, l := range leases {
		if l != nil {
			require.Nil(t, winner, "exactly one concurrent request may hold the probe")
			winner = contexts[i]
			require.Nil(t, errs[i])
		} else {
			require.NotNil(t, errs[i])
			require.Equal(t, http.StatusServiceUnavailable, errs[i].StatusCode)
		}
	}
	require.NotNil(t, winner)
	require.Equal(t, 1, ChannelInFlight(11))
	ch, selErr = ReacquireSelectedRouteChannel(c, 11, key.model)
	require.Nil(t, ch, "formal reacquisition must rescan while the probe is occupied")
	require.Nil(t, selErr)
	old := currentRouteProbe(winner)
	ReleaseRouteChannel(winner)
	require.Zero(t, ChannelInFlight(11), "unsent attempt releases probe and channel")
	ch, selErr = ReacquireSelectedRouteChannel(c, 11, key.model)
	require.Nil(t, selErr)
	require.Equal(t, 11, ch.Id)
	newProbe := currentRouteProbe(c)
	require.NotNil(t, newProbe)
	routeObservations.releaseProbe(old)
	routeObservations.recordBreaker(old.epoch, key, old, true)
	class, _ := routeObservations.availability(key)
	require.Equal(t, routeProbeBusy, class, "late completion cannot clear a subsequent probe")
	routeObservations.invalidate(11)
	for range routeBreakerOpenFailures {
		recordBreaker(11, key.model, false)
	}
	routeObservations.recordBreaker(newProbe.epoch, key, newProbe, true)
	ReleaseRouteChannel(c)
	class, _ = routeObservations.availability(key)
	require.Equal(t, routeOpen, class, "late completion cannot close the replaced configuration")
	require.Zero(t, ChannelInFlight(11))
}

func TestRouteAdmissionFullBeforeCoolingFallback(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "m")
	createRouteChannel(t, db, 22, "m")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("max_concurrency", 1).Error)
	permit, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11, MaxConcurrency: 1})
	require.NoError(t, err)
	defer permit.Release()
	routeObservations.coolDown(routeObservations.epoch, 22, time.Now().Add(time.Minute).UnixMilli())
	_, failure := SelectChannelForRequest(routeTestContext(t), "m", nil)
	require.Equal(t, http.StatusTooManyRequests, failure.StatusCode)
	for range routeBreakerOpenFailures {
		recordBreaker(11, "m", false)
	}
	ch, failure := SelectChannelForRequest(routeTestContext(t), "m", nil)
	require.Nil(t, failure)
	require.Equal(t, 22, ch.Id)
}

func TestPreferEscapesPauseAndStrictKeepsChannel(t *testing.T) {
	for _, mode := range []string{"prefer", "strict"} {
		t.Run(mode, func(t *testing.T) {
			setupAffinityChannels(t)
			resetRouteObservations(t)
			c, key := enableRouteAffinity(t, mode, 22)
			routeObservations.coolDown(routeObservations.epoch, 22, time.Now().Add(time.Minute).UnixMilli())
			channel, err := SelectChannelForRequest(c, "route-model", nil)
			require.Nil(t, err)
			want := 11
			if mode == "strict" {
				want = 22
			}
			require.Equal(t, want, channel.Id)
			bound, found := affinityBinding(t, key)
			require.True(t, found)
			require.Equal(t, 22, bound, "selection alone cannot clear or overwrite the binding")
		})
	}
}
