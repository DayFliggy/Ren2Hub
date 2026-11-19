package service

import (
	"fmt"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func cacheTestRequest(i int) routeCacheRequest {
	return routeCacheRequest{mode: "explicit", ttl: time.Hour, session: [16]byte{byte(i), byte(i >> 8), 1}}
}
func TestRouteCacheRequestHints(t *testing.T) {
	read := func(tenant int, body string) routeCacheRequest {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("id", tenant)
		return routeCacheContext(c)
	}
	a := read(12, `{"prompt_cache_key":"conversation","prompt_cache_retention":"24h"}`)
	require.True(t, a.identifiable())
	require.NotEqual(t, a.session, read(13, `{"prompt_cache_key":"conversation","prompt_cache_retention":"24h"}`).session)
	b := read(12, `{"messages":[{"content":[{"cache_control":{"type":"ephemeral","ttl":"1h"}}]}],"metadata":{"user_id":"user_a_session_repeat"}}`)
	require.Equal(t, time.Hour, b.ttl)
	require.Equal(t, "explicit", b.mode)
	require.True(t, b.identifiable())
	unknown := read(12, `{"prompt_cache_key":"conversation"}`)
	require.False(t, unknown.identifiable())
	resource := read(12, `{"cachedContent":"owned/resource","prompt_cache_key":"k","prompt_cache_retention":"24h"}`)
	require.True(t, resource.resource)
}

func TestRouteCacheQualityComparableSessions(t *testing.T) {
	s := newCacheStore()
	start := time.Unix(1800000000, 0)
	sample := func(at time.Time, offset, sessions int, lo, hi int64) {
		for i := range sessions {
			ctx := cacheTestRequest(i + offset)
			for round := range 5 {
				tm := at.Add(time.Duration(round) * time.Second)
				for channel, rate := range []int64{lo, hi} {
					s.record(ctx, "m", routeObservationKey{id: channel + 1, model: "m"}, tm, tm.Add(time.Millisecond), rate, 100, true, "auto")
				}
			}
		}
	}
	sample(start, 0, 20, 20, 80)
	s.evaluate(start.Add(cachePeriod))
	ctx := cacheTestRequest(0)
	bands := s.bands(ctx, "m", 0, false, start.Add(cachePeriod))
	require.True(t, bands[1].Known)
	require.Zero(t, bands[1].Band, "two fresh evaluations required")
	s.evaluate(start.Add(2 * cachePeriod))
	require.Zero(t, s.bands(ctx, "m", 0, false, start.Add(2*cachePeriod))[1].Band, "duplicate observations cannot downgrade")
	sample(start.Add(2*cachePeriod+time.Minute), 25, 20, 20, 80)
	s.evaluate(start.Add(3 * cachePeriod))
	require.Equal(t, 2, s.bands(ctx, "m", 0, false, start.Add(3*cachePeriod))[1].Band)
	require.Zero(t, s.bands(ctx, "m", 0, false, start.Add(3*cachePeriod))[2].Band)
	sample(start.Add(4*cachePeriod), 50, 20, 80, 80)
	s.evaluate(start.Add(5 * cachePeriod))
	require.Equal(t, 2, s.bands(ctx, "m", 0, false, start.Add(5*cachePeriod))[1].Band, "minimum residence")
	s.evaluate(start.Add(6 * cachePeriod))
	require.Zero(t, s.bands(ctx, "m", 0, false, start.Add(6*cachePeriod))[1].Band, "new equivalent evidence should recover")
	require.Nil(t, s.bands(cacheTestRequest(200), "m", 0, false, start.Add(6*cachePeriod)), "first visits cannot inherit warm ranking")
	s.invalidate(2)
	require.Nil(t, s.bands(ctx, "m", 0, false, start.Add(6*cachePeriod)))
}

// TTL and channel both determine whether a simulated provider can reuse a prompt.
func TestRouteCacheStatefulWorkload(t *testing.T) {
	s := newCacheStore()
	start := time.Unix(1800000000, 0)
	type providerKey struct{ id, session int }
	warmUntil := map[providerKey]time.Time{}
	tm := start
	send := func(id, i int, size int64, valid bool) {
		req := cacheTestRequest(i)
		req.ttl = 30 * time.Minute
		k := providerKey{id, i}
		read := int64(0)
		if tm.Before(warmUntil[k]) {
			read = size * 4 / 5
		}
		warmUntil[k] = tm.Add(req.ttl)
		s.record(req, "m", routeObservationKey{id: id, model: "m"}, tm, tm.Add(time.Millisecond), read, size, valid, "auto")
		tm = tm.Add(100 * time.Millisecond)
	}
	for batch := range 2 {
		tm = start.Add(time.Duration(batch) * cachePeriod)
		for i := range 60 {
			// Channel 1 handles mostly repeat visits; channel 2 sees mostly cold traffic.
			for range 5 {
				send(1, batch*200+i, 4000, true)
			}
			send(2, batch*200+i, 4000, true)
			if i%4 == 0 {
				send(2, batch*200+i, 4000, true)
			}
			send(3, batch*200+i, 4000, false)
		}
		s.evaluate(tm.Add(time.Millisecond))
	}
	result := s.diagnostics().Bands
	require.Len(t, result, 3)
	require.True(t, result[0].Known)
	require.True(t, result[1].Known)
	require.Greater(t, result[0].TokenRate-result[1].TokenRate, .35)
	require.Zero(t, result[0].Band)
	require.Zero(t, result[1].Band, "corrected for different warm mix")
	require.False(t, result[2].Known)
	send(1, 9999, 20000000, true)
	s.evaluate(tm.Add(cachePeriod))
	require.False(t, s.diagnostics().Bands[0].Known, "one extremely long input cannot change routing")
}

func TestRouteCacheExplorationBudget(t *testing.T) {
	s := newCacheStore()
	now := time.Unix(1800000000, 0)
	pool := []int{1, 2}
	explorer := func(i int) int { return s.explore(cacheTestRequest(i), "m", 0, false, pool, []int{2}, now) }
	for i := range 19 {
		require.Zero(t, explorer(i))
	}
	require.Equal(t, 2, explorer(20))
	group := cacheGroup{"m", 0, false, "explicit"}
	s.result[group] = map[int]routeCacheBand{2: {Known: true}}
	s.at = now
	require.Nil(t, s.bands(cacheTestRequest(20), "m", 0, false, now), "a reservation before dispatch cannot warm a cache")
	for range 10 {
		require.Equal(t, 2, explorer(20))
	}
	for i := range 30 {
		require.Zero(t, explorer(100+i), "over budget permits existing sessions only")
	}
	now = now.Add(2 * time.Hour)
	require.Zero(t, explorer(20), "expired assignments cannot survive TTL")
	anonymous := newCacheStore()
	for range 19 {
		require.Zero(t, anonymous.explore(routeCacheRequest{}, "m", 0, false, pool, []int{2}, now))
	}
	require.Equal(t, 2, anonymous.explore(routeCacheRequest{}, "m", 0, false, pool, []int{2}, now))
	require.Empty(t, anonymous.visits)
	require.Zero(t, s.explore(routeCacheRequest{resource: true}, "m", 0, false, pool, pool, now))
}

func TestRouteCacheCapacityAndEvaluation(t *testing.T) {
	s := newCacheStore()
	now := time.Now().Truncate(cachePeriod)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := range cacheRowsLimit + 1 {
		key := routeObservationKey{id: i + 1, model: fmt.Sprint(i)}
		s.record(cacheTestRequest(i), key.model, key, now, now.Add(time.Millisecond), 0, 128, true, "auto")
	}
	t0 := time.Now()
	s.evaluate(now.Add(time.Second))
	elapsed := time.Since(t0)
	runtime.ReadMemStats(&after)
	d := s.diagnostics()
	require.Len(t, d.Bands, cacheRowsLimit)
	require.Equal(t, uint64(1), d.Evictions)
	require.LessOrEqual(t, d.Cells, d.CellLimit)
	require.LessOrEqual(t, d.VisitCount, d.VisitLimit)
	t.Logf("rows=%d active_cells=%d visits=%d allocated_bytes=%d evaluation=%s", len(d.Bands), d.Cells, d.VisitCount, after.TotalAlloc-before.TotalAlloc, elapsed)
}

func TestRouteCacheSaturatedWindow(t *testing.T) {
	s := newCacheStore()
	now := time.Now().Truncate(cachePeriod)
	runtime.GC()
	var initial, retained runtime.MemStats
	runtime.ReadMemStats(&initial)
	// Each cell uses a real session; both first and repeat visits are kept.
	const channels = 16
	sessions := cacheCellsLimit / (channels * 2)
	for channel := range channels {
		key := routeObservationKey{id: channel + 1, model: "m"}
		for i := range sessions {
			req := cacheTestRequest(i)
			s.record(req, "m", key, now, now.Add(time.Millisecond), 0, 128, true, "auto")
			s.record(req, "m", key, now.Add(time.Second), now.Add(2*time.Second), 96, 128, true, "auto")
		}
	}
	s.evaluate(now.Add(3 * time.Second))
	d := s.diagnostics()
	require.Equal(t, cacheCellsLimit, d.Cells)
	require.Equal(t, cacheVisitsLimit, d.VisitCount)
	for _, entry := range d.Bands {
		require.True(t, entry.Known)
		require.Zero(t, entry.Band)
	}
	s.record(cacheTestRequest(sessions+2), "m", routeObservationKey{id: 1, model: "m"}, now.Add(4*time.Second), now.Add(5*time.Second), 64, 128, true, "auto")
	s.evaluate(now.Add(cachePeriod))
	d = s.diagnostics()
	require.False(t, d.Bands[0].Known, "capacity exhaustion cannot publish biased evidence")
	require.Greater(t, d.Evictions, uint64(0))
	runtime.GC()
	runtime.ReadMemStats(&retained)
	t.Logf("saturated cells=%d visits=%d, incremental retained heap=%d, evaluation=%dms", d.Cells, d.VisitCount, int64(retained.HeapAlloc)-int64(initial.HeapAlloc), d.EvaluationMS)
	runtime.KeepAlive(s)
}
