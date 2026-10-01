package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"runtime"
	"slices"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

const (
	opsSchemaVersion  = 1
	opsSampleInterval = 10 * time.Second
	opsRetention      = 24 * time.Hour
	opsProbeTimeout   = 2 * time.Second
)

var ErrOpsWindowInvalid = errors.New("window must be 1h, 6h or 24h")
var errOpsNotSampled = errors.New("ops monitor has not sampled yet")

// opsWindows keeps every window at 180 series points.
var opsWindows = map[string]struct{ span, bucket time.Duration }{
	"1h":  {time.Hour, 20 * time.Second},
	"6h":  {6 * time.Hour, 2 * time.Minute},
	"24h": {24 * time.Hour, 8 * time.Minute},
}

// opsPoint is one compact ring sample. Gauges are NaN when unmeasured;
// counters are process-lifetime totals whose deltas become rates.
type opsPoint struct {
	ts                                      int64
	cpu, memory, disk, netUp, netDown       float64
	goroutines, heapAlloc, inFlight, queued float64
	requests, successes, granted, rejected  uint64
}

type opsSnapshot struct {
	sampledAt int64
	resources OpsResources
	gateway   OpsGateway
	storage   OpsStorage
}

// opsSampler holds the last 24 hours of process samples. It lives in memory:
// a restart starts a new history, which the page states.
type opsSampler struct {
	mu      sync.RWMutex
	ring    []opsPoint
	next    int
	full    bool
	latest  *opsSnapshot
	failing map[string]bool
}

var opsMonitor = newOpsSampler(int(opsRetention / opsSampleInterval))
var opsMonitorOnce sync.Once

func newOpsSampler(capacity int) *opsSampler {
	return &opsSampler{ring: make([]opsPoint, capacity), failing: map[string]bool{}}
}

// StartOpsMonitor samples once synchronously, so the overview is available as
// soon as the server listens, then keeps sampling in the background.
func StartOpsMonitor() {
	opsMonitorOnce.Do(func() {
		opsMonitor.record(opsMonitor.sample(context.Background()))
		common.BackgroundWorkers.Go(func(ctx context.Context) {
			for common.WaitBackgroundInterval(ctx, opsSampleInterval) {
				opsMonitor.record(opsMonitor.sample(ctx))
			}
		})
	})
}

// ReadOpsOverview serves the latest background sample. Admin polling never
// adds database or Redis probes of its own.
func ReadOpsOverview(window string) (OpsOverview, error) {
	spec, ok := opsWindows[window]
	if !ok {
		return OpsOverview{}, ErrOpsWindowInvalid
	}
	now := time.Now()
	// One extra bucket supplies the baseline for the first bucket's rates.
	latest, points := opsMonitor.read(now.Add(-spec.span - spec.bucket).Unix())
	if latest == nil {
		return OpsOverview{}, errOpsNotSampled
	}
	gateway := latest.gateway
	gateway.SuccessRate = common.GetRequestSuccessRate()
	gateway.GrantedPerMinute, gateway.RejectedPerMinute = opsRecentRates(points)
	checks := deriveOpsChecks(latest.resources, gateway, latest.storage, common.IsMasterNode)
	return OpsOverview{
		SchemaVersion: opsSchemaVersion,
		SampledAt:     latest.sampledAt,
		Status:        rollupOpsStatus(checks),
		Checks:        checks,
		Runtime:       opsRuntimeInfo(now, gateway.Admission.Draining),
		Resources:     latest.resources,
		Gateway:       gateway,
		Storage:       latest.storage,
		Series:        buildOpsSeries(points, now.Add(-spec.span).Unix(), window, int64(spec.bucket.Seconds())),
	}, nil
}

func opsRuntimeInfo(now time.Time, draining bool) OpsRuntime {
	return OpsRuntime{
		Version: common.Version, GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		NodeName: common.NodeName, StartedAt: common.StartTime, UptimeSeconds: now.Unix() - common.StartTime,
		Master: common.IsMasterNode, Draining: draining,
	}
}

func (s *opsSampler) record(point opsPoint, snapshot opsSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ring[s.next] = point
	s.next = (s.next + 1) % len(s.ring)
	s.full = s.full || s.next == 0
	s.latest = &snapshot
}

// read returns the latest snapshot and the samples at or after since, oldest
// first. It walks back from the newest sample, so cost follows the window.
func (s *opsSampler) read(since int64) (*opsSnapshot, []opsPoint) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.latest == nil {
		return nil, nil
	}
	count := s.next
	if s.full {
		count = len(s.ring)
	}
	points := make([]opsPoint, 0, min(count, 2048))
	for i := 1; i <= count; i++ {
		point := s.ring[(s.next-i+len(s.ring))%len(s.ring)]
		if point.ts < since {
			break
		}
		points = append(points, point)
	}
	slices.Reverse(points)
	latest := *s.latest
	return &latest, points
}

func (s *opsSampler) sample(ctx context.Context) (opsPoint, opsSnapshot) {
	status := common.GetSystemStatus()
	monitor := common.GetPerformanceMonitorConfig()
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	admission := gatewayAdmission.Metrics()
	requests, successes := common.RequestOutcomeTotals()
	snapshot := opsSnapshot{
		sampledAt: time.Now().Unix(),
		resources: OpsResources{
			MonitorEnabled: monitor.Enabled,
			CPU:            OpsPercent{Threshold: monitor.CPUThreshold},
			Memory:         OpsUsage{Threshold: monitor.MemoryThreshold},
			Disk:           OpsUsage{Threshold: monitor.DiskThreshold, Path: common.DiskSpacePath()},
			Process: OpsProcess{
				Goroutines: runtime.NumGoroutine(), HeapAllocBytes: memStats.HeapAlloc, HeapSysBytes: memStats.HeapSys,
				GCCount: memStats.NumGC, LastGCPauseMs: float64(memStats.PauseNs[(memStats.NumGC+255)%256]) / 1e6,
			},
		},
		gateway: OpsGateway{
			Admission: admission, WaitBucketBoundsMS: opsWaitBucketBounds(), RetryBudget: gatewayRetries.Snapshot(),
			UserQueue: gatewayMetrics.user.snapshot(), ChannelQueue: gatewayMetrics.channel.snapshot(),
		},
		storage: OpsStorage{Database: s.probeDatabase(ctx, "database", model.DB, common.MainDatabaseType()), Redis: s.probeRedis(ctx)},
	}
	if status.CPUAvailable {
		snapshot.resources.CPU.Percent = opsValue(status.CPUUsage)
	}
	if status.MemoryAvailable {
		memory := &snapshot.resources.Memory
		memory.Percent, memory.UsedBytes, memory.TotalBytes = opsValue(status.MemoryUsage), &status.MemoryUsedBytes, &status.MemoryTotalBytes
	}
	if status.DiskAvailable {
		disk := &snapshot.resources.Disk
		disk.Percent, disk.UsedBytes, disk.TotalBytes = opsValue(status.DiskUsage), &status.DiskUsedBytes, &status.DiskTotalBytes
	}
	if status.Network.Available {
		snapshot.resources.Network = OpsNetwork{UpMbps: opsValue(status.Network.UpMbps), DownMbps: opsValue(status.Network.DownMbps)}
	}
	if model.LOG_DB != nil && model.LOG_DB != model.DB {
		probe := s.probeDatabase(ctx, "log_database", model.LOG_DB, common.LogDatabaseType())
		snapshot.storage.LogDatabase = &probe
	}
	resources := snapshot.resources
	point := opsPoint{
		ts: snapshot.sampledAt, cpu: opsGauge(resources.CPU.Percent), memory: opsGauge(resources.Memory.Percent),
		disk: opsGauge(resources.Disk.Percent), netUp: opsGauge(resources.Network.UpMbps), netDown: opsGauge(resources.Network.DownMbps),
		goroutines: float64(resources.Process.Goroutines), heapAlloc: float64(memStats.HeapAlloc),
		inFlight: float64(admission.InFlight), queued: float64(admission.Queued),
		requests: requests, successes: successes, granted: admission.Granted, rejected: opsRejected(admission),
	}
	return point, snapshot
}

// opsRejected counts gateway-side refusals; client cancellations are excluded.
func opsRejected(admission AdmissionMetrics) uint64 {
	return admission.QueueFull + admission.TotalQueueFull + admission.Timeouts
}

// JSON has no +Inf, so the unbounded last bucket is reported as null.
func opsWaitBucketBounds() []*int64 {
	bounds := make([]*int64, len(AdmissionWaitBucketBoundsMS))
	for i, bound := range AdmissionWaitBucketBoundsMS {
		if bound != math.MaxInt64 {
			bounds[i] = &bound
		}
	}
	return bounds
}

func opsValue(value float64) *float64 { return &value }

func opsGauge(value *float64) float64 {
	if value == nil {
		return math.NaN()
	}
	return *value
}

func (s *opsSampler) probeDatabase(ctx context.Context, name string, db *gorm.DB, kind common.DatabaseType) OpsDatabaseProbe {
	probe := OpsDatabaseProbe{Kind: string(kind)}
	var sqlDB interface {
		PingContext(context.Context) error
	}
	if db != nil {
		if handle, err := db.DB(); err == nil {
			stats := handle.Stats()
			probe.Open, probe.InUse, probe.Idle = stats.OpenConnections, stats.InUse, stats.Idle
			probe.MaxOpen, probe.WaitCount = stats.MaxOpenConnections, stats.WaitCount
			sqlDB = handle
		}
	}
	probe.Status, probe.LatencyMs, probe.Error = s.ping(ctx, name, func(ctx context.Context) error {
		if sqlDB == nil {
			return errors.New("database handle unavailable")
		}
		return sqlDB.PingContext(ctx)
	})
	return probe
}

func (s *opsSampler) probeRedis(ctx context.Context) OpsRedisProbe {
	client := common.RDB
	if !common.RedisEnabled || client == nil {
		return OpsRedisProbe{Status: OpsStatusUnknown}
	}
	probe := OpsRedisProbe{Enabled: true}
	if stats := client.PoolStats(); stats != nil {
		probe.TotalConns, probe.IdleConns, probe.StaleConns = stats.TotalConns, stats.IdleConns, stats.StaleConns
	}
	probe.Status, probe.LatencyMs, probe.Error = s.ping(ctx, "redis", func(ctx context.Context) error {
		return client.Ping(ctx).Err()
	})
	return probe
}

// ping bounds a dependency check and logs only the transition into failure,
// so an outage does not write one line per sample.
func (s *opsSampler) ping(parent context.Context, name string, check func(context.Context) error) (OpsStatus, *float64, string) {
	ctx, cancel := context.WithTimeout(parent, opsProbeTimeout)
	defer cancel()
	started := time.Now()
	err := check(ctx)
	latency := float64(time.Since(started).Microseconds()) / 1000
	if err == nil {
		if s.failing[name] {
			logger.LogInfo(context.Background(), fmt.Sprintf("ops monitor: %s probe recovered", name))
		}
		delete(s.failing, name)
		return OpsStatusOK, &latency, ""
	}
	if !s.failing[name] {
		logger.LogWarn(context.Background(), fmt.Sprintf("ops monitor: %s probe failed: %v", name, err))
	}
	s.failing[name] = true
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return OpsStatusCritical, nil, "timeout"
	}
	return OpsStatusCritical, nil, "unavailable"
}

// opsRecentRates compares the newest sample with the newest one at least a
// minute older; without such a pair the rate is not yet measured.
func opsRecentRates(points []opsPoint) (granted, rejected *float64) {
	if len(points) < 2 {
		return nil, nil
	}
	last := points[len(points)-1]
	for i := len(points) - 2; i >= 0; i-- {
		if last.ts-points[i].ts >= 60 {
			minutes := float64(last.ts-points[i].ts) / 60
			return opsPerMinute(points[i].granted, last.granted, minutes), opsPerMinute(points[i].rejected, last.rejected, minutes)
		}
	}
	return nil, nil
}

func opsPerMinute(before, after uint64, minutes float64) *float64 {
	if minutes <= 0 || after < before {
		return nil
	}
	return opsRound(float64(after-before) / minutes)
}

func opsRound(value float64) *float64 {
	rounded := math.Round(value*1000) / 1000
	return &rounded
}

// buildOpsSeries averages gauges per bucket and turns counter deltas against
// the previous bucket's last sample into per-minute rates. Samples before the
// window only seed that baseline. Buckets without samples are omitted, so gaps
// stay gaps instead of reading as zero.
func buildOpsSeries(points []opsPoint, windowStart int64, window string, bucket int64) OpsSeries {
	series := OpsSeries{Window: window, BucketSeconds: bucket, Points: []OpsSeriesPoint{}}
	firstBucket := windowStart - windowStart%bucket
	var baseline *opsPoint
	for start := 0; start < len(points); {
		bucketTs := points[start].ts - points[start].ts%bucket
		end := start + 1
		for end < len(points) && points[end].ts-points[end].ts%bucket == bucketTs {
			end++
		}
		group := points[start:end]
		if bucketTs >= firstBucket {
			series.Points = append(series.Points, opsSeriesPoint(bucketTs, group, baseline))
		}
		baseline = &points[end-1]
		start = end
	}
	return series
}

func opsSeriesPoint(bucketTs int64, group []opsPoint, baseline *opsPoint) OpsSeriesPoint {
	var sums, counts [9]float64
	for _, point := range group {
		for i, value := range [9]float64{point.cpu, point.memory, point.disk, point.netUp, point.netDown, point.goroutines, point.heapAlloc, point.inFlight, point.queued} {
			if !math.IsNaN(value) {
				sums[i] += value
				counts[i]++
			}
		}
	}
	var gauges [9]*float64
	for i := range sums {
		if counts[i] > 0 {
			gauges[i] = opsRound(sums[i] / counts[i])
		}
	}
	point := OpsSeriesPoint{
		Ts: bucketTs, CPU: gauges[0], Memory: gauges[1], Disk: gauges[2], NetUpMbps: gauges[3], NetDownMbps: gauges[4],
		Goroutines: gauges[5], HeapAllocBytes: gauges[6], InFlight: gauges[7], Queued: gauges[8],
	}
	from, last := group[0], group[len(group)-1]
	if baseline != nil {
		from = *baseline
	}
	minutes := float64(last.ts-from.ts) / 60
	point.RequestsPerMinute = opsPerMinute(from.requests, last.requests, minutes)
	point.GrantedPerMinute = opsPerMinute(from.granted, last.granted, minutes)
	point.RejectedPerMinute = opsPerMinute(from.rejected, last.rejected, minutes)
	if last.requests > from.requests && last.successes >= from.successes {
		point.SuccessRate = opsRound(float64(last.successes-from.successes) / float64(last.requests-from.requests) * 100)
	}
	return point
}
