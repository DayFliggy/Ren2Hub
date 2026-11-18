package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
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
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{""}}})
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusServiceUnavailable, Header: http.Header{"Retry-After": []string{"30"}}})
	require.Empty(t, routeObservations.cooldowns, "neither an explicitly empty header nor a non-429 starts a cooldown")
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"30"}}})
	longCooldown := routeObservations.cooldowns[11]
	ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests})
	require.Equal(t, longCooldown, routeObservations.cooldowns[11], "a headerless 429 cannot shorten the existing cooldown")
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

func TestHeaderless429CooldownExpiresAfterFiveSeconds(t *testing.T) {
	resetRouteObservations(t)
	synctest.Test(t, func(t *testing.T) {
		headerless429 := func(keyIndex int, modelName string) {
			c := routeTestContext(t)
			BeginRouteObservation(c)
			common.SetContextKey(c, constant.ContextKeyChannelId, 11)
			common.SetContextKey(c, constant.ContextKeyChannelIsMultiKey, true)
			common.SetContextKey(c, constant.ContextKeyChannelMultiKeyIndex, keyIndex)
			MarkRouteAttemptSent(c, &relaycommon.RelayInfo{OriginModelName: modelName})
			ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests})
		}

		headerless429(0, "first-model")
		time.Sleep(4 * time.Second)
		health, _ := routeObservations.routeHealth(11, "second-model")
		require.Equal(t, routeWorstHealth, health, "the other model shares the first key's cooldown")
		health, _ = routeObservations.routeHealth(22, "first-model")
		require.Zero(t, health, "another channel serving the same model remains healthy")

		headerless429(1, "second-model")
		time.Sleep(5*time.Second - time.Millisecond)
		health, _ = routeObservations.routeHealth(11, "first-model")
		require.Equal(t, routeWorstHealth, health, "the second key renews the cooldown for the first model")
		time.Sleep(time.Millisecond)
		health, _ = routeObservations.routeHealth(11, "first-model")
		require.Zero(t, health)
	})
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
	for _, value := range []string{"", "  ", "0", "-5", "-9223372036854775807", "soon", now.Add(-time.Minute).Format(http.TimeFormat)} {
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

// Midjourney 直接用 http.Client 发送，不经过 relay/channel 的 doRequest，冷却挂点要单独接上。
func TestMidjourneyRetryAfterStartsCooldown(t *testing.T) {
	setupRouteTest(t)
	resetRouteObservations(t)
	initDefaultHTTPClientFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":23,"description":"queue full"}`))
	}))
	t.Cleanup(upstream.Close)
	c := routeTestContext(t)
	c.Request = httptest.NewRequest(http.MethodGet, "/mj/task/abc/fetch", nil)
	BeginRouteObservation(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	info := &relaycommon.RelayInfo{OriginModelName: "mj_imagine", StartTime: time.Now()}
	_, _, err := DoMidjourneyHttpRequest(c, info, time.Second, upstream.URL)
	require.NoError(t, err)
	health, _ := routeObservations.routeHealth(11, "")
	require.Equal(t, routeWorstHealth, health)
}

// epoch 变更表有上限：满容量时清空并抬高过期下限，之前开始的尝试对所有渠道都不再写入。
func TestRouteEpochTableIsBounded(t *testing.T) {
	resetRouteObservations(t)
	previous := routeObservationCapacity
	routeObservationCapacity = 3
	t.Cleanup(func() { routeObservationCapacity = previous })
	routeObservations.mu.Lock()
	before := routeObservations.epoch
	routeObservations.mu.Unlock()
	for id := 1; id <= 10; id++ {
		routeObservations.invalidate(id)
	}
	require.LessOrEqual(t, len(routeObservations.changed), 3)
	recordRouteObservation(before, 50, "m", 1, false, true, 100)
	require.Zero(t, routeObservations.get(50, "m", 1, false).Samples, "an attempt older than the floor is stale for every channel")
	routeObservations.mu.Lock()
	current := routeObservations.epoch
	routeObservations.mu.Unlock()
	recordRouteObservation(current, 50, "m", 1, false, true, 100)
	require.Equal(t, 1, routeObservations.get(50, "m", 1, false).Samples)
	recordRouteObservation(current-1, 10, "m", 1, false, true, 100)
	require.Zero(t, routeObservations.get(10, "m", 1, false).Samples, "the latest invalidation still rejects older attempts on its channel")
}

// 满容量清空必须精确：清空前判为过期的 (epoch, 渠道) 清空后仍过期；清空时的当前 epoch
// 开始于所有已清除的失效之后，对没有再变更的渠道仍然有效。
func TestRouteEpochTableClearKeepsEveryStaleAttemptStale(t *testing.T) {
	resetRouteObservations(t)
	previous := routeObservationCapacity
	routeObservationCapacity = 3
	t.Cleanup(func() { routeObservationCapacity = previous })
	for _, id := range []int{1, 2, 3, 2} {
		routeObservations.invalidate(id)
	}
	routeObservations.mu.Lock()
	current := routeObservations.epoch
	before := map[[2]uint64]bool{}
	for epoch := uint64(0); epoch <= current; epoch++ {
		for id := 1; id <= 5; id++ {
			before[[2]uint64{epoch, uint64(id)}] = routeObservations.staleLocked(epoch, id)
		}
	}
	routeObservations.mu.Unlock()

	routeObservations.invalidate(4)
	routeObservations.mu.Lock()
	defer routeObservations.mu.Unlock()
	require.Len(t, routeObservations.changed, 1, "the full table was cleared")
	for key, stale := range before {
		epoch, id := key[0], int(key[1])
		after := routeObservations.staleLocked(epoch, id)
		if stale {
			require.True(t, after, "epoch %d on channel %d was stale before the clear", epoch, id)
		}
		if epoch == current && id != 4 {
			require.False(t, after, "epoch %d started after every cleared invalidation of channel %d", epoch, id)
		}
	}
	require.True(t, routeObservations.staleLocked(current, 4), "the channel invalidated by the clearing call rejects older attempts")
}
