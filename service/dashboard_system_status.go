package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/shirou/gopsutil/cpu"
	"golang.org/x/sync/singleflight"
)

// DashboardSystemStatus is the projection of the instance observation that
// every signed-in user may read. Storage probes, check reasons, thresholds,
// paths and process internals stay on the Root-only /api/performance routes.
type DashboardSystemStatus struct {
	Status      string               `json:"status"`
	Stale       bool                 `json:"stale"`
	SampledAt   *int64               `json:"sampled_at"`
	CPU         DashboardResource    `json:"cpu"`
	Memory      DashboardResource    `json:"memory"`
	Disk        DashboardResource    `json:"disk"`
	Load        *DashboardLoad       `json:"load"`
	Traffic     DashboardTraffic     `json:"traffic"`
	SuccessRate DashboardSuccessRate `json:"success_rate"`
	// StorageStatus is the worst database/Redis probe verdict: ok, critical,
	// stale or unknown. It names the cause of a critical status that no tile
	// shows; the Root-only "storage" object with probe names, reasons and
	// latencies stays off this projection.
	StorageStatus string `json:"storage_status"`
}

// DashboardResource.State is ok, warning or critical for a checked reading,
// unchecked for a reading whose threshold check is off, or unknown, stale or
// disabled (monitoring off) without a reading.
type DashboardResource struct {
	Available  bool     `json:"available"`
	State      string   `json:"state"`
	Scope      string   `json:"scope"`
	Percent    *float64 `json:"percent"`
	UsedBytes  *uint64  `json:"used_bytes"`
	TotalBytes *uint64  `json:"total_bytes"`
}

// DashboardLoad is the kernel run-queue average. It is host-wide even inside a
// container, so it is normalized by host logical cores; Cores is null when the
// count cannot be read.
type DashboardLoad struct {
	Scope   string  `json:"scope"`
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
	Cores   *int    `json:"cores"`
}

type DashboardTraffic struct {
	Scope         string                  `json:"scope"`
	RequestMbps   *float64                `json:"request_mbps"`
	ResponseMbps  *float64                `json:"response_mbps"`
	BucketSeconds int64                   `json:"bucket_seconds"`
	Points        []DashboardTrafficPoint `json:"points"`
}

type DashboardTrafficPoint struct {
	Ts           int64    `json:"ts"`
	RequestMbps  *float64 `json:"request_mbps"`
	ResponseMbps *float64 `json:"response_mbps"`
}

// DashboardSuccessRate states why Rate is null: perf metrics disabled, no
// requests in the window, or a failed query. Rate is set only for "ok".
type DashboardSuccessRate struct {
	State       string   `json:"state"`
	Rate        *float64 `json:"rate"`
	WindowStart *int64   `json:"window_start"`
	WindowEnd   *int64   `json:"window_end"`
	CheckedAt   int64    `json:"checked_at"`
}

const (
	dashboardSuccessRateHours    = 24
	dashboardSuccessRateTTL      = time.Minute
	dashboardSuccessRateErrorTTL = 10 * time.Second
)

// The upstream perf-metrics query takes no context. A waiter gives up after
// this deadline so a blocked database cannot hold the resource readings; the
// shared query keeps running and refills the cache when it returns.
var dashboardSuccessRateWait = 3 * time.Second

var dashboardSuccessRateCache struct {
	sync.Mutex
	value   DashboardSuccessRate
	expires time.Time
}
var dashboardSuccessRateRefresh singleflight.Group
var queryDashboardHourly = perfmetrics.QueryHourlyAll

// Logical cores rarely change at runtime; reading /proc/cpuinfo per poll is
// unnecessary. A read error is logged once and reported as null.
var dashboardHostCores = sync.OnceValue(func() *int {
	count, err := cpu.Counts(true)
	if err != nil || count <= 0 {
		common.SysError(fmt.Sprintf("host cpu core count unavailable: count=%d err=%v", count, err))
		return nil
	}
	return &count
})

// ReadDashboardSystemStatus reads the cached observation; only the success
// rate may touch the database, at most once per TTL across all callers.
func ReadDashboardSystemStatus(ctx context.Context) (DashboardSystemStatus, error) {
	observation, err := ReadPerformanceObservation("1h")
	if err != nil {
		return DashboardSystemStatus{}, err
	}
	rate, err := dashboardSuccessRate(ctx)
	if err != nil {
		return DashboardSystemStatus{}, err
	}
	return newDashboardSystemStatus(observation, dashboardLoad(), rate), nil
}

func newDashboardSystemStatus(observation PerformanceObservation, load *DashboardLoad, rate DashboardSuccessRate) DashboardSystemStatus {
	traffic := DashboardTraffic{
		Scope: observation.Traffic.Scope, RequestMbps: observation.Traffic.RequestMbps,
		ResponseMbps: observation.Traffic.ResponseMbps, BucketSeconds: observation.Series.BucketSeconds,
		Points: make([]DashboardTrafficPoint, 0, len(observation.Series.Points)),
	}
	for _, point := range observation.Series.Points {
		traffic.Points = append(traffic.Points, DashboardTrafficPoint{Ts: point.Ts, RequestMbps: point.RequestMbps, ResponseMbps: point.ResponseMbps})
	}
	checks := make(map[string]string, len(observation.Checks))
	for _, check := range observation.Checks {
		checks[check.ID] = check.Status
	}
	return DashboardSystemStatus{
		Status: observation.Status, Stale: observation.Stale, SampledAt: observation.SampledAt,
		CPU:    dashboardResource(observation.Resources.CPU, checks["cpu"]),
		Memory: dashboardResource(observation.Resources.Memory, checks["memory"]),
		Disk:   dashboardResource(observation.Resources.Disk, checks["disk"]),
		Load:   load, Traffic: traffic, SuccessRate: rate, StorageStatus: dashboardStorageStatus(checks),
	}
}

var dashboardStorageRank = map[string]int{"ok": 0, "unknown": 1, "stale": 2, "critical": 3}

// dashboardStorageStatus reduces the database and Redis checks to their worst
// verdict; a disabled Redis is not a dependency. Without a checked database, or
// with a status it does not know, the verdict is unknown, never ok.
func dashboardStorageStatus(checks map[string]string) string {
	if status, ok := checks["database"]; !ok || status == "disabled" {
		return "unknown"
	}
	worst := "ok"
	for _, id := range []string{"database", "log_database", "redis"} {
		status, ok := checks[id]
		if !ok || status == "disabled" {
			continue
		}
		if _, known := dashboardStorageRank[status]; !known {
			status = "unknown"
		}
		if dashboardStorageRank[status] > dashboardStorageRank[worst] {
			worst = status
		}
	}
	return worst
}

// dashboardResource carries the health check's verdict on a measured resource,
// so the tile colour and the status pill come from the same threshold rule.
// A threshold of 0 turns off the check, not the measurement: the reading stays,
// marked "unchecked" so it does not pass for a checked "ok".
func dashboardResource(resource ObservationResource, check string) DashboardResource {
	state := resource.State
	if state == "ok" && (check == "warning" || check == "critical") {
		state = check
	}
	if state == "ok" && check == "disabled" {
		state = "unchecked"
	}
	return DashboardResource{
		Available: resource.Available, State: state, Scope: resource.Scope,
		Percent: resource.Percent, UsedBytes: resource.UsedBytes, TotalBytes: resource.TotalBytes,
	}
}

var dashboardLoadCache struct {
	sync.Mutex
	value   *DashboardLoad
	expires time.Time
}

// dashboardLoad samples at most once per observation interval, so polling by
// many users neither rereads the kernel file nor repeats a failure log.
func dashboardLoad() *DashboardLoad {
	dashboardLoadCache.Lock()
	defer dashboardLoadCache.Unlock()
	if time.Now().Before(dashboardLoadCache.expires) {
		return dashboardLoadCache.value
	}
	dashboardLoadCache.value, dashboardLoadCache.expires = nil, time.Now().Add(observationInterval)
	if one, five, fifteen, ok := sampleDashboardLoad(); ok {
		dashboardLoadCache.value = &DashboardLoad{Scope: "host", One: one, Five: five, Fifteen: fifteen, Cores: dashboardHostCores()}
	}
	return dashboardLoadCache.value
}

// dashboardSuccessRate shares one perf-metrics query per TTL across all
// callers. A failed query is logged and reported as state "error" for a short
// TTL, so the resource readings stay visible while the failure is explicit. A
// query still running past the wait deadline is reported as "error" uncached.
func dashboardSuccessRate(ctx context.Context) (DashboardSuccessRate, error) {
	read := func() (DashboardSuccessRate, bool) {
		dashboardSuccessRateCache.Lock()
		defer dashboardSuccessRateCache.Unlock()
		return dashboardSuccessRateCache.value, time.Now().Before(dashboardSuccessRateCache.expires)
	}
	if value, ok := read(); ok {
		return value, nil
	}
	result := dashboardSuccessRateRefresh.DoChan("success_rate", func() (any, error) {
		if value, ok := read(); ok {
			return value, nil
		}
		value, ttl := loadDashboardSuccessRate(time.Now())
		dashboardSuccessRateCache.Lock()
		dashboardSuccessRateCache.value = value
		dashboardSuccessRateCache.expires = time.Now().Add(ttl)
		dashboardSuccessRateCache.Unlock()
		return value, nil
	})
	timer := time.NewTimer(dashboardSuccessRateWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return DashboardSuccessRate{}, ctx.Err()
	case <-timer.C:
		common.SysError(fmt.Sprintf("dashboard success rate query exceeded %s", dashboardSuccessRateWait))
		return DashboardSuccessRate{State: "error", CheckedAt: time.Now().Unix()}, nil
	case response := <-result:
		return response.Val.(DashboardSuccessRate), nil
	}
}

func loadDashboardSuccessRate(now time.Time) (DashboardSuccessRate, time.Duration) {
	value := DashboardSuccessRate{CheckedAt: now.Unix()}
	hourly, err := queryDashboardHourly(dashboardSuccessRateHours)
	if err != nil {
		common.SysError(fmt.Sprintf("dashboard success rate query failed: %v", err))
		value.State = "error"
		return value, dashboardSuccessRateErrorTTL
	}
	value.WindowStart, value.WindowEnd = &hourly.WindowStart, &hourly.WindowEnd
	switch {
	case !hourly.Enabled:
		value.State = "disabled"
	case hourly.Summary == nil:
		value.State = "no_requests"
	default:
		value.State, value.Rate = "ok", &hourly.Summary.SuccessRate
	}
	return value, dashboardSuccessRateTTL
}
