package service

import (
	"context"
	"errors"
	"testing"
	"time"

	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
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
