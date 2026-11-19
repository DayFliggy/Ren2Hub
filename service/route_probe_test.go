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
