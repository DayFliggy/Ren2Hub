package service

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func breakerWindowTotal(key routeBreakerKey) int {
	routeObservations.mu.Lock()
	defer routeObservations.mu.Unlock()
	total := 0
	for _, bucket := range routeObservations.breakers[key].window {
		total += int(bucket.total)
	}
	return total
}

// A streaming probe has served the request once its first content event is
// out. It settles the breaker there, so the next request can probe while the
// first stream still runs; the end of that stream is not counted again.
func TestStreamProbeSettlesAtFirstOutput(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	key := routeBreakerKey{11, "route-model"}
	content := `{"choices":[{"delta":{"content":"hi"}}]}`
	halfOpen := func() {
		routeObservations.invalidate(11)
		for range routeBreakerOpenFailures {
			recordBreaker(11, key.model, false)
		}
		expireBreakerCooldown(t, 11, key.model)
	}
	probeStream := func() (*gin.Context, *relaycommon.RelayInfo) {
		t.Helper()
		c := routeTestContext(t)
		selected, selErr := SelectChannelForRequest(c, key.model, nil)
		require.Nil(t, selErr, "the next request must be able to probe")
		require.NotNil(t, currentRouteProbe(c))
		common.SetContextKey(c, constant.ContextKeyChannelId, selected.Id)
		info := &relaycommon.RelayInfo{OriginModelName: key.model, RelayMode: 1, IsStream: true, StartTime: time.Now()}
		MarkRouteAttemptSent(c, info)
		return c, info
	}

	halfOpen()
	var running []*gin.Context
	var infos []*relaycommon.RelayInfo
	for i := range routeBreakerCloseSuccesses {
		class, _ := routeObservations.availability(key)
		require.Equal(t, routeAvailable, class, "probe %d", i+1)
		c, info := probeStream()
		ObserveRouteStreamOutput(c, content)
		running, infos = append(running, c), append(infos, info)
	}
	require.Zero(t, routeObservations.breakers[key].openUntil, "three first outputs close the breaker")
	require.Equal(t, 1, breakerWindowTotal(key))
	require.Equal(t, routeBreakerCloseSuccesses, ChannelInFlight(11), "channel slots stay held until each stream ends")
	for i, c := range running {
		ObserveRouteStreamOutput(c, content)
		FinishRouteAttempt(c, infos[i], nil)
	}
	require.Equal(t, 1, breakerWindowTotal(key), "stream ends do not count the settled probes again")
	require.Zero(t, ChannelInFlight(11))

	t.Run("failure before first output reopens", func(t *testing.T) {
		halfOpen()
		c, info := probeStream()
		FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("upstream"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))
		class, _ := routeObservations.availability(key)
		require.Equal(t, routeOpen, class)
	})

	t.Run("first output after a configuration change writes nothing", func(t *testing.T) {
		halfOpen()
		c, info := probeStream()
		routeObservations.invalidate(11)
		ObserveRouteStreamOutput(c, content)
		FinishRouteAttempt(c, info, nil)
		_, present := routeObservations.breakers[key]
		require.False(t, present)
	})

	t.Run("one probe in flight under a burst", func(t *testing.T) {
		halfOpen()
		first, info := probeStream()
		ObserveRouteStreamOutput(first, content)
		contexts := make([]*gin.Context, 100)
		for i := range contexts {
			contexts[i] = routeTestContext(t)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, c := range contexts {
			wg.Go(func() {
				<-start
				_, _ = SelectChannelForRequest(c, key.model, nil)
			})
		}
		close(start)
		wg.Wait()
		holders := 0
		for _, c := range contexts {
			if currentRouteProbe(c) != nil {
				holders++
			}
			ReleaseRouteChannel(c)
		}
		require.Equal(t, 1, holders)
		FinishRouteAttempt(first, info, nil)
	})
}

// The end of an attempt may run while its first output is settling the probe.
// The end waits for that settlement before it releases the probe, so the
// success counts exactly once and the next request can probe.
func TestStreamProbeSettlementCountsOnceWhenFinishRaces(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	key := routeBreakerKey{11, "route-model"}
	content := `{"choices":[{"delta":{"content":"hi"}}]}`
	halfOpen := func() {
		routeObservations.invalidate(11)
		for range routeBreakerOpenFailures {
			recordBreaker(11, key.model, false)
		}
		expireBreakerCooldown(t, 11, key.model)
	}
	probeStream := func() (*gin.Context, *relaycommon.RelayInfo) {
		t.Helper()
		c := routeTestContext(t)
		selected, selErr := SelectChannelForRequest(c, key.model, nil)
		require.Nil(t, selErr)
		require.NotNil(t, currentRouteProbe(c), "the next request must be able to probe")
		common.SetContextKey(c, constant.ContextKeyChannelId, selected.Id)
		info := &relaycommon.RelayInfo{OriginModelName: key.model, RelayMode: 1, IsStream: true, StartTime: time.Now()}
		MarkRouteAttemptSent(c, info)
		return c, info
	}
	breaker := func() routeBreakerState {
		routeObservations.mu.Lock()
		defer routeObservations.mu.Unlock()
		return routeObservations.breakers[key]
	}
	// pauseSettlement runs the real observer until its settlement has won the
	// write, then the real Finish; it returns the release of the paused write.
	pauseSettlement := func(c *gin.Context, info *relaycommon.RelayInfo, whilePaused func()) {
		t.Helper()
		entered, release := make(chan struct{}), make(chan struct{})
		streamProbeSettleTestHook = func() {
			close(entered)
			<-release
		}
		t.Cleanup(func() { streamProbeSettleTestHook = nil })
		observed := make(chan struct{})
		go func() {
			defer close(observed)
			ObserveRouteStreamOutput(c, content)
		}()
		<-entered
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			FinishRouteAttempt(c, info, nil)
		}()
		select {
		case <-finished:
		case <-time.After(100 * time.Millisecond):
		}
		whilePaused()
		close(release)
		<-observed
		<-finished
		streamProbeSettleTestHook = nil
	}

	t.Run("finish during the settlement write", func(t *testing.T) {
		halfOpen()
		c, info := probeStream()
		pauseSettlement(c, info, func() {})
		state := breaker()
		require.Equal(t, 1, state.successes, "the first output settles the probe exactly once")
		require.Nil(t, state.probe)
		require.Zero(t, ChannelInFlight(11))
		next, _ := probeStream()
		FinishRouteAttempt(next, &relaycommon.RelayInfo{OriginModelName: key.model, IsStream: true}, nil)
	})

	t.Run("finish wins before the first output", func(t *testing.T) {
		halfOpen()
		c, info := probeStream()
		value, _ := c.Get("route_attempt")
		attempt := value.(*routeAttempt)
		FinishRouteAttempt(c, info, nil)
		attempt.markOutput(time.Now())
		state := breaker()
		require.Equal(t, 1, state.successes)
		require.Nil(t, state.probe)
		next, _ := probeStream()
		FinishRouteAttempt(next, &relaycommon.RelayInfo{OriginModelName: key.model, IsStream: true}, nil)
	})

	t.Run("configuration published during the settlement", func(t *testing.T) {
		halfOpen()
		c, info := probeStream()
		pauseSettlement(c, info, func() { routeObservations.invalidate(11) })
		_, present := routeObservations.breakers[key]
		require.False(t, present, "a stale settlement writes nothing")
		require.Zero(t, ChannelInFlight(11))
	})
}

// Some handlers learn that the response streams only when the upstream answers
// with SSE, after dispatch. Reaching the stream observer is what makes the
// attempt a stream, so the probe still settles at the first output.
func TestStreamProbeSettlesWhenStreamIsKnownAfterDispatch(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	key := routeBreakerKey{11, "route-model"}
	routeObservations.invalidate(11)
	for range routeBreakerOpenFailures {
		recordBreaker(11, key.model, false)
	}
	expireBreakerCooldown(t, 11, key.model)

	c := routeTestContext(t)
	selected, selErr := SelectChannelForRequest(c, key.model, nil)
	require.Nil(t, selErr)
	require.NotNil(t, currentRouteProbe(c))
	common.SetContextKey(c, constant.ContextKeyChannelId, selected.Id)
	info := &relaycommon.RelayInfo{OriginModelName: key.model, RelayMode: 1, IsStream: false, StartTime: time.Now()}
	MarkRouteAttemptSent(c, info)
	// relay/compatible_handler.go after an SSE Content-Type
	info.IsStream = true
	ObserveRouteStreamOutput(c, `{"choices":[{"delta":{"content":"hi"}}]}`)

	next := routeTestContext(t)
	_, selErr = SelectChannelForRequest(next, key.model, nil)
	require.Nil(t, selErr, "the settled probe lets the next request probe")
	require.NotNil(t, currentRouteProbe(next))
	require.Equal(t, 2, ChannelInFlight(11), "the first stream still holds its channel slot")
	ReleaseRouteChannel(next)
	FinishRouteAttempt(c, info, nil)
	require.Zero(t, ChannelInFlight(11))
}
