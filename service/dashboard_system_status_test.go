package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardSystemStatusProjectsObservationWithoutInventingValues(t *testing.T) {
	cpu, memory, up := 12.5, 40.0, 0.25
	used, total := uint64(4<<30), uint64(16<<30)
	sampledAt := int64(1792583995)
	observation := emptyPerformanceObservation()
	observation.Status, observation.SampledAt = "warning", &sampledAt
	observation.Resources.CPU = ObservationResource{Available: true, State: "ok", Scope: "container", Percent: &cpu, Threshold: 90}
	observation.Resources.Memory = ObservationResource{Available: true, State: "ok", Scope: "host", Percent: &memory, UsedBytes: &used, TotalBytes: &total}
	observation.Traffic = ObservationTraffic{Scope: "application_http_body", RequestMbps: &up}
	observation.Series = ObservationSeries{BucketSeconds: 60, Points: []ObservationSeriesPoint{{Ts: 60, RequestMbps: &up, CPU: &cpu}, {Ts: 120}}}

	status := newDashboardSystemStatus(observation, nil, DashboardSuccessRate{State: "no_requests"})
	require.Equal(t, "warning", status.Status)
	require.Equal(t, DashboardResource{Available: true, State: "ok", Scope: "container", Percent: &cpu}, status.CPU)
	require.Equal(t, &used, status.Memory.UsedBytes)
	require.Equal(t, DashboardResource{Scope: "unknown", State: "unknown"}, status.Disk, "unsampled disk stays unknown")
	require.Nil(t, status.Traffic.ResponseMbps, "a missing direction is not reported as idle")
	require.Equal(t, []DashboardTrafficPoint{{Ts: 60, RequestMbps: &up}, {Ts: 120}}, status.Traffic.Points, "empty buckets keep their gap")
	require.Nil(t, status.Load)
}

func TestDashboardSuccessRateDistinguishesEmptyDisabledAndFailedWindows(t *testing.T) {
	previous := queryDashboardHourly
	t.Cleanup(func() { queryDashboardHourly = previous })
	now := time.Unix(1792583995, 0)
	for _, test := range []struct {
		name   string
		result perfmetrics.HourlyResult
		err    error
		state  string
		rate   *float64
		ttl    time.Duration
	}{
		{"disabled", perfmetrics.HourlyResult{Enabled: false, Summary: &perfmetrics.Summary{SuccessRate: 50}}, nil, "disabled", nil, dashboardSuccessRateTTL},
		{"no requests", perfmetrics.HourlyResult{Enabled: true}, nil, "no_requests", nil, dashboardSuccessRateTTL},
		{"zero success", perfmetrics.HourlyResult{Enabled: true, Summary: &perfmetrics.Summary{SuccessRate: 0}}, nil, "ok", new(float64), dashboardSuccessRateTTL},
		{"query failure", perfmetrics.HourlyResult{}, errors.New("database unavailable"), "error", nil, dashboardSuccessRateErrorTTL},
	} {
		t.Run(test.name, func(t *testing.T) {
			queryDashboardHourly = func(hours int) (perfmetrics.HourlyResult, error) {
				require.Equal(t, dashboardSuccessRateHours, hours)
				return test.result, test.err
			}
			value, ttl := loadDashboardSuccessRate(now)
			require.Equal(t, test.state, value.State)
			require.Equal(t, test.rate, value.Rate)
			require.Equal(t, test.ttl, ttl)
			require.Equal(t, now.Unix(), value.CheckedAt)
		})
	}
}

func TestDashboardSuccessRateSharesOneQueryPerTTL(t *testing.T) {
	previous := queryDashboardHourly
	t.Cleanup(func() {
		queryDashboardHourly = previous
		dashboardSuccessRateCache.Lock()
		dashboardSuccessRateCache.expires = time.Time{}
		dashboardSuccessRateCache.Unlock()
	})
	dashboardSuccessRateCache.Lock()
	dashboardSuccessRateCache.expires = time.Time{}
	dashboardSuccessRateCache.Unlock()
	calls := 0
	queryDashboardHourly = func(int) (perfmetrics.HourlyResult, error) {
		calls++
		return perfmetrics.HourlyResult{Enabled: true, Summary: &perfmetrics.Summary{SuccessRate: 99.5}}, nil
	}
	for range 3 {
		value, err := dashboardSuccessRate(context.Background())
		require.NoError(t, err)
		require.Equal(t, 99.5, *value.Rate)
	}
	require.Equal(t, 1, calls)
}

func TestDashboardSuccessRateBlockedQueryDoesNotHoldWaiters(t *testing.T) {
	previousQuery, previousWait := queryDashboardHourly, dashboardSuccessRateWait
	resetCache := func() {
		dashboardSuccessRateCache.Lock()
		dashboardSuccessRateCache.expires = time.Time{}
		dashboardSuccessRateCache.Unlock()
	}
	release := make(chan struct{})
	t.Cleanup(func() {
		queryDashboardHourly, dashboardSuccessRateWait = previousQuery, previousWait
		resetCache()
	})
	resetCache()
	dashboardSuccessRateWait = 50 * time.Millisecond
	var calls atomic.Int32
	done := make(chan struct{})
	queryDashboardHourly = func(int) (perfmetrics.HourlyResult, error) {
		calls.Add(1)
		defer close(done)
		<-release
		return perfmetrics.HourlyResult{Enabled: true, Summary: &perfmetrics.Summary{SuccessRate: 98}}, nil
	}

	var waiters sync.WaitGroup
	for range 4 {
		waiters.Add(1)
		go func() {
			defer waiters.Done()
			value, err := dashboardSuccessRate(context.Background())
			assert.NoError(t, err)
			assert.Equal(t, "error", value.State)
			assert.Nil(t, value.Rate)
		}()
	}
	waiters.Wait()
	require.Equal(t, int32(1), calls.Load(), "concurrent waiters share one query")

	close(release)
	<-done
	require.Eventually(t, func() bool {
		value, err := dashboardSuccessRate(context.Background())
		return err == nil && value.State == "ok" && *value.Rate == 98
	}, time.Second, 10*time.Millisecond, "the finished query fills the cache")
	require.Equal(t, int32(1), calls.Load())
}

func TestDashboardSuccessRateCancelledWaiterReturnsContextError(t *testing.T) {
	previous := queryDashboardHourly
	release, done := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		// Let the shared query finish before resetting what it writes.
		close(release)
		<-done
		queryDashboardHourly = previous
		dashboardSuccessRateCache.Lock()
		dashboardSuccessRateCache.expires = time.Time{}
		dashboardSuccessRateCache.Unlock()
	})
	dashboardSuccessRateCache.Lock()
	dashboardSuccessRateCache.expires = time.Time{}
	dashboardSuccessRateCache.Unlock()
	queryDashboardHourly = func(int) (perfmetrics.HourlyResult, error) {
		defer close(done)
		<-release
		return perfmetrics.HourlyResult{Enabled: true}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := dashboardSuccessRate(ctx)
	require.ErrorIs(t, err, context.Canceled)
}
