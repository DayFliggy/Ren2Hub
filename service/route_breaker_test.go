package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

func recordBreaker(channelID int, modelName string, success bool) {
	routeObservations.mu.Lock()
	epoch := routeObservations.epoch
	routeObservations.mu.Unlock()
	recordRouteBreaker(epoch, channelID, modelName, success)
}

// expireBreakerCooldown 把打开的熔断推到冷却结束，进入半开。
func expireBreakerCooldown(t *testing.T, channelID int, modelName string) {
	t.Helper()
	key := routeBreakerKey{channelID, modelName}
	state, ok := routeObservations.breakers[key]
	require.True(t, ok)
	require.NotZero(t, state.openUntil)
	state.openUntil = time.Now().Add(-time.Millisecond).UnixMilli()
	routeObservations.breakers[key] = state
}

func TestRouteBreakerOpensOnlyAfterConsecutiveFailures(t *testing.T) {
	resetRouteObservations(t)
	// 旧的错误率分档在样本达标后，一次失败就把渠道打到最差档。
	for range routeMinimumSamples {
		RecordRouteObservation(11, "m", 1, false, true, 100)
		recordBreaker(11, "m", true)
	}
	RecordRouteObservation(11, "m", 1, false, false, 0)
	recordBreaker(11, "m", false)
	health, weight := routeObservations.routeHealth(11, "m")
	require.Zero(t, health)
	require.Equal(t, 1.0, weight)
	for range routeBreakerOpenFailures - 2 {
		recordBreaker(11, "m", false)
	}
	recordBreaker(11, "m", true) // 一次成功清零连续失败计数
	for range routeBreakerOpenFailures - 1 {
		recordBreaker(11, "m", false)
	}
	health, _ = routeObservations.routeHealth(11, "m")
	require.Zero(t, health)
	recordBreaker(11, "m", false)
	health, _ = routeObservations.routeHealth(11, "m")
	require.Equal(t, routeWorstHealth, health)
}

func TestRouteBreakerHalfOpenWeightAndRecovery(t *testing.T) {
	resetRouteObservations(t)
	key := routeBreakerKey{11, "m"}
	open := func() {
		for range routeBreakerOpenFailures {
			recordBreaker(11, "m", false)
		}
	}
	open()
	openUntil := routeObservations.breakers[key].openUntil
	recordBreaker(11, "m", true)
	recordBreaker(11, "m", false)
	require.Equal(t, openUntil, routeObservations.breakers[key].openUntil, "results during the cooldown neither close nor extend it")
	health, _ := routeObservations.routeHealth(11, "m")
	require.Equal(t, routeWorstHealth, health)

	expireBreakerCooldown(t, 11, "m")
	health, weight := routeObservations.routeHealth(11, "m")
	require.Zero(t, health)
	require.Equal(t, routeHalfOpenWeight, weight)
	recordBreaker(11, "m", false)
	health, _ = routeObservations.routeHealth(11, "m")
	require.Equal(t, routeWorstHealth, health, "a half-open failure reopens immediately")

	expireBreakerCooldown(t, 11, "m")
	for range routeBreakerCloseSuccesses - 1 {
		recordBreaker(11, "m", true)
		_, weight = routeObservations.routeHealth(11, "m")
		require.Equal(t, routeHalfOpenWeight, weight)
	}
	recordBreaker(11, "m", true)
	health, weight = routeObservations.routeHealth(11, "m")
	require.Zero(t, health)
	require.Equal(t, 1.0, weight)
	require.Empty(t, routeObservations.breakers, "a closed breaker keeps no state")

	open()
	state := routeObservations.breakers[key]
	state.updatedAt = time.Now().Add(-routeObservationTTL - time.Second).UnixMilli()
	routeObservations.breakers[key] = state
	health, _ = routeObservations.routeHealth(11, "m")
	require.Zero(t, health, "breaker state expires with the observation TTL")
}

func TestRouteBreakerCountsPerActualModelAndStreamOutputIsSuccess(t *testing.T) {
	setupRouteTest(t)
	resetRouteObservations(t)
	c := routeTestContext(t)
	BeginRouteObservation(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	c.Set("model_mapping", `{"alias":"actual"}`)
	failure := types.NewOpenAIError(errors.New("upstream"), types.ErrorCodeBadResponseStatusCode, http.StatusInternalServerError)
	info := &relaycommon.RelayInfo{OriginModelName: "alias", RelayMode: 1, IsStream: true, StartTime: time.Now()}
	attempt := func(output string) {
		MarkRouteAttemptSent(c, info)
		if output != "" {
			ObserveRouteStreamOutput(c, output)
		}
		FinishRouteAttempt(c, info, failure)
	}
	for range routeBreakerOpenFailures - 1 {
		attempt("")
	}
	require.Equal(t, routeBreakerOpenFailures-1, routeObservations.breakers[routeBreakerKey{11, "actual"}].failures)
	attempt(`{"choices":[{"delta":{"content":"partial"}}]}`)
	require.NotContains(t, routeObservations.breakers, routeBreakerKey{11, "actual"}, "a stream that delivered content was served")
	for range routeBreakerOpenFailures {
		attempt(`{"type":"response.created"}`)
	}
	health, _ := routeObservations.routeHealth(11, "actual")
	require.Equal(t, routeWorstHealth, health, "metadata-only events are not output")
	health, _ = routeObservations.routeHealth(11, "other")
	require.Zero(t, health, "another model on the same channel is unaffected")
	health, _ = routeObservations.routeHealth(22, "actual")
	require.Zero(t, health)
}

func TestRetryAfterCooldownDemotesChannelWithoutRemovingIt(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	c := routeTestContext(t)
	BeginRouteObservation(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	info := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, StartTime: time.Now()}
	MarkRouteAttemptSent(c, info)
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{}})
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": []string{"30"}}})
	require.Empty(t, routeObservations.cooldowns, "only a 429 with Retry-After starts a cooldown")
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"30"}}})
	FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("rate limited"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests))
	for range 20 {
		selected, selectErr := SelectChannelForRequest(routeTestContext(t), "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id, "a cooling channel drops to the worst tier for every draw")
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("status", common.ChannelStatusManuallyDisabled).Error)
	selected, selectErr := SelectChannelForRequest(routeTestContext(t), "route-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 11, selected.Id, "demotion never removes the channel")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("status", common.ChannelStatusEnabled).Error)
	saveRouteTest(t, 11, 22)
	selected, selectErr = SelectChannelForRequest(routeTestContext(t), "route-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 11, selected.Id, "a manual list keeps its order")
}

func TestRetryAfterParsing(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	header := func(value string) http.Header { return http.Header{"Retry-After": []string{value}} }
	until, ok := retryAfterUntil(header("30"), now)
	require.True(t, ok)
	require.Equal(t, now.Add(30*time.Second).UnixMilli(), until)
	until, ok = retryAfterUntil(header(now.Add(2*time.Minute).Format(http.TimeFormat)), now)
	require.True(t, ok)
	require.Equal(t, now.Add(2*time.Minute).UnixMilli(), until)
	until, ok = retryAfterUntil(header("999999999999"), now)
	require.True(t, ok)
	require.Equal(t, now.Add(routeObservationTTL).UnixMilli(), until, "the cooldown is capped by the observation TTL")
	for _, value := range []string{"", "0", "-5", "soon", now.Add(-time.Minute).Format(http.TimeFormat)} {
		_, ok = retryAfterUntil(header(value), now)
		require.False(t, ok, value)
	}
}

// B8：表满时为新键腾位置，而不是静默丢弃新键。
func TestRouteObservationStoreEvictsWhenFull(t *testing.T) {
	resetRouteObservations(t)
	previous := routeObservationCapacity
	routeObservationCapacity = 3
	t.Cleanup(func() { routeObservationCapacity = previous })
	key := func(name string) routeObservationKey { return routeObservationKey{11, name, 1, false} }
	for _, name := range []string{"a", "b", "c"} {
		RecordRouteObservation(11, name, 1, false, true, 100)
	}
	stale := routeObservations.rows[key("a")]
	stale.UpdatedAt = time.Now().Add(-routeObservationTTL - time.Second).UnixMilli()
	routeObservations.rows[key("a")] = stale
	RecordRouteObservation(11, "d", 1, false, true, 100)
	require.Len(t, routeObservations.rows, 3)
	require.NotContains(t, routeObservations.rows, key("a"), "expired entries go first")
	require.Contains(t, routeObservations.rows, key("d"))
	oldest := routeObservations.rows[key("b")]
	oldest.UpdatedAt -= 1000
	routeObservations.rows[key("b")] = oldest
	RecordRouteObservation(11, "e", 1, false, true, 100)
	require.Len(t, routeObservations.rows, 3)
	require.NotContains(t, routeObservations.rows, key("b"), "then the least recently updated entry")
	require.Contains(t, routeObservations.rows, key("e"), "a new key is never silently dropped")
	for _, name := range []string{"x", "y", "z", "w"} {
		recordBreaker(22, name, false)
	}
	require.Len(t, routeObservations.breakers, 3)
	require.Contains(t, routeObservations.breakers, routeBreakerKey{22, "w"})
}
