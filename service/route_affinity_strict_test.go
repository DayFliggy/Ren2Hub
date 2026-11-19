package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

// strict session affinity treats an open breaker as "bound channel
// unavailable", as NewAPI does for an unavailable affinity channel: 503 and the
// binding is cleared unless KeepOnChannelDisabled. A half-open channel without
// a probe is still served as the probe, and a channel that only cools down
// after a 429 is still used because strict has no other channel.
func TestStrictAffinityHonorsOpenBreaker(t *testing.T) {
	bound := routeBreakerKey{22, "route-model"}
	openBreaker := func() {
		for range routeBreakerOpenFailures {
			recordBreaker(22, bound.model, false)
		}
	}
	for _, tc := range []struct {
		name string
		keep bool
	}{{"binding cleared", false}, {"binding kept", true}} {
		t.Run("open breaker "+tc.name, func(t *testing.T) {
			setupAffinityChannels(t)
			resetRouteObservations(t)
			c, key := enableRouteAffinity(t, "strict", 22)
			operation_setting.GetChannelAffinitySetting().KeepOnChannelDisabled = tc.keep
			openBreaker()
			selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
			require.Nil(t, selected)
			require.NotNil(t, selectErr)
			require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
			require.Equal(t, "strict_session_binding_unavailable", selectErr.Message)
			require.NotEmpty(t, c.Writer.Header().Get("Retry-After"))
			require.Zero(t, ChannelInFlight(22))
			require.Zero(t, ChannelInFlight(11), "strict never switches channels within the request")
			_, found := affinityBinding(t, key)
			require.Equal(t, tc.keep, found)
		})
	}

	t.Run("half-open probe busy rejects", func(t *testing.T) {
		setupAffinityChannels(t)
		resetRouteObservations(t)
		openBreaker()
		expireBreakerCooldown(t, 22, bound.model)
		holder, key := enableRouteAffinity(t, "strict", 22)
		probe, selectErr := SelectChannelForRequest(holder, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, probe.Id)
		require.NotNil(t, currentRouteProbe(holder))
		c := routeTestContext(t)
		c.Request.Header.Set("X-Session", t.Name())
		_, selectErr = SelectChannelForRequest(c, "route-model", nil)
		require.NotNil(t, selectErr)
		require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
		_, found := affinityBinding(t, key)
		require.False(t, found)
	})

	t.Run("half-open without probe is served as the probe once", func(t *testing.T) {
		setupAffinityChannels(t)
		resetRouteObservations(t)
		openBreaker()
		expireBreakerCooldown(t, 22, bound.model)
		c, key := enableRouteAffinity(t, "strict", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		require.NotNil(t, currentRouteProbe(c))
		common.SetContextKey(c, constant.ContextKeyChannelId, 22)
		info := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, StartTime: time.Now()}
		MarkRouteAttemptSent(c, info)
		FinishRouteAttempt(c, info, nil)
		require.Equal(t, 1, routeObservations.breakers[bound].successes)
		boundID, found := affinityBinding(t, key)
		require.True(t, found)
		require.Equal(t, 22, boundID)
	})

	t.Run("plugin reacquisition applies the same rule", func(t *testing.T) {
		setupAffinityChannels(t)
		resetRouteObservations(t)
		c, _ := enableRouteAffinity(t, "strict", 22)
		selected, selectErr := PreselectTaskPluginEndpointChannel(c, "route-model")
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		ReleaseRouteChannel(c)
		openBreaker()
		reacquired, selectErr := ReacquireSelectedRouteChannel(c, 22, "route-model")
		require.Nil(t, selectErr)
		require.Nil(t, reacquired, "the open bound channel is not admitted; the caller selects again")
		require.Zero(t, ChannelInFlight(22))
	})

	t.Run("cooling only still sends", func(t *testing.T) {
		setupAffinityChannels(t)
		resetRouteObservations(t)
		c, key := enableRouteAffinity(t, "strict", 22)
		routeObservations.coolDown(routeObservations.epoch, 22, routeCooldown{until: time.Now().Add(time.Minute).UnixMilli(), source: "retry_after"})
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		boundID, found := affinityBinding(t, key)
		require.True(t, found)
		require.Equal(t, 22, boundID)
	})
}
