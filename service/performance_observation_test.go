package service

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestObservationResourceAvailabilityScopeAndStaleness(t *testing.T) {
	now := time.Unix(1800000000, 0)
	config := common.PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90}
	status := common.SystemStatus{
		SampledAt: now.Add(-5 * time.Second), CPUAvailable: true, CPUUsage: 12, CPUScope: "container",
		MemoryAvailable: true, MemoryUsage: 25, MemoryScope: "container", MemoryUsedBytes: 256, MemoryTotalBytes: 1024,
	}
	resources := observationResources(status, config, now)
	require.Equal(t, "container", resources.CPU.Scope)
	require.Equal(t, 12., *resources.CPU.Percent)
	require.Equal(t, uint64(256), *resources.Memory.UsedBytes)
	require.Equal(t, uint64(1024), *resources.Memory.TotalBytes)
	require.Nil(t, resources.Disk.Percent)
	require.False(t, resources.Disk.Available)
	require.Equal(t, "unknown", resources.Disk.State)
	status.CPUScope, status.MemoryScope = "host", "host"
	require.Equal(t, "host", observationResources(status, config, now).CPU.Scope)
	resources = observationResources(status, config, now.Add(31*time.Second))
	require.Nil(t, resources.CPU.Percent)
	require.Equal(t, "stale", resources.CPU.State)
	resources = observationResources(status, common.PerformanceMonitorConfig{}, now)
	require.Equal(t, "disabled", resources.Memory.State)
	require.Nil(t, resources.Memory.TotalBytes)
	resources = observationResources(common.SystemStatus{}, config, now)
	require.Nil(t, resources.CPU.SampledAt)
	require.Nil(t, resources.CPU.Percent)
}

func TestObservationGCBeforeFirstCollectionAndRingWrap(t *testing.T) {
	mem := runtime.MemStats{HeapAlloc: 123, HeapSys: 456}
	require.Nil(t, observationProcess(mem, 10).LastGCPauseMs)
	mem.NumGC = 257
	mem.PauseNs[0] = 1_500_000
	process := observationProcess(mem, 11)
	require.Equal(t, 1.5, *process.LastGCPauseMs)
	require.Equal(t, uint64(123), process.HeapAllocBytes)
	require.Equal(t, 11, process.Goroutines)
}

func TestObservationTrafficBaselineRules(t *testing.T) {
	sampler := trafficSampler{}
	now := time.Unix(1800000000, 0)
	for _, step := range []struct {
		name            string
		at              time.Duration
		in, out         uint64
		inMbps, outMbps *float64
	}{
		{"the first sample only sets the baseline", 0, 100, 200, nil, nil},
		{"Mbps is delta bytes x 8 / seconds / 1e6, per direction", 10 * time.Second, 1_250_100, 2_500_200, observationPtr(1.), observationPtr(2.)},
		{"a real zero delta is idle, not missing", 20 * time.Second, 1_250_100, 2_500_200, observationPtr(0.), observationPtr(0.)},
		{"a decrease in either counter resets", 30 * time.Second, 1_250_100, 1, nil, nil},
		{"the next sample measures from the reset", 40 * time.Second, 1_375_100, 250_001, observationPtr(.1), observationPtr(.2)},
		{"zero elapsed resets", 40 * time.Second, 1_400_000, 300_000, nil, nil},
		{"clock reversal resets", 35 * time.Second, 1_500_000, 400_000, nil, nil},
		{"elapsed equal to observationMaxAge still measures", 65 * time.Second, 5_250_000, 400_000, observationPtr(1.), observationPtr(0.)},
		{"a gap over observationMaxAge resets", 96 * time.Second, 6_000_000, 500_000, nil, nil},
		{"after the gap a rate needs two samples", 106 * time.Second, 7_250_000, 500_000, observationPtr(1.), observationPtr(0.)},
	} {
		inMbps, outMbps := sampler.sample(now.Add(step.at), step.in, step.out, "")
		require.Equal(t, step.inMbps, inMbps, step.name)
		require.Equal(t, step.outMbps, outMbps, step.name)
	}
}

func writeObservationNetDev(t *testing.T, file, iface string, rx, tx uint64) {
	t.Helper()
	content := fmt.Sprintf("Inter-| Receive | Transmit\n face |bytes packets|bytes packets\n%s: %d 0 0 0 0 0 0 0 %d 0 0 0 0 0 0 0\n", iface, rx, tx)
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
}

// captureObservationLogs collects the SysLog and SysError lines written while
// the test runs.
func captureObservationLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	common.LogWriterMu.Lock()
	previousOut, previousErr := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter, gin.DefaultErrorWriter = &buffer, &buffer
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter, gin.DefaultErrorWriter = previousOut, previousErr
		common.LogWriterMu.Unlock()
	})
	return &buffer
}

func TestObservationTrafficFailureRequiresNewBaseline(t *testing.T) {
	logs := captureObservationLogs(t)
	file := filepath.Join(t.TempDir(), "dev")
	source := common.TrafficSource{Scope: common.TrafficScopeHostInterface, Interface: "eth0", NetDev: file}
	sampler := newObservationSampler(1)
	now := time.Unix(1800000000, 0)

	writeObservationNetDev(t, file, "eth0", 1_000_000, 2_000_000)
	require.Nil(t, sampler.sampleTraffic(source, now).InboundMbps, "the first sample only sets the baseline")
	writeObservationNetDev(t, file, "eth0", 2_250_000, 4_500_000)
	healthy := sampler.sampleTraffic(source, now.Add(10*time.Second))
	require.Equal(t, ObservationTraffic{Scope: "host_interface", Interface: "eth0", InboundMbps: observationPtr(1.), OutboundMbps: observationPtr(2.)}, healthy,
		"inbound is host rx and outbound is host tx")

	require.NoError(t, os.Remove(file))
	unreadable := sampler.sampleTraffic(source, now.Add(20*time.Second))
	require.Equal(t, ObservationTraffic{Scope: "host_interface", Interface: "eth0", Reason: "host_counters_unreadable"}, unreadable, "a reason means both rates are null")
	require.Equal(t, "host_counters_unreadable", sampler.sampleTraffic(source, now.Add(25*time.Second)).Reason)
	writeObservationNetDev(t, file, "eth1", 9_000_000, 9_000_000)
	missing := sampler.sampleTraffic(source, now.Add(27*time.Second))
	require.Equal(t, "host_interface_missing", missing.Reason, "another interface is never read instead")
	require.Nil(t, missing.InboundMbps)
	require.Nil(t, missing.OutboundMbps)

	// The counters grew during the outage. A rate from the old baseline would
	// be (3_500_000-2_250_000) bytes over 20 s; recovery must not report it.
	writeObservationNetDev(t, file, "eth0", 3_500_000, 7_000_000)
	recovered := sampler.sampleTraffic(source, now.Add(30*time.Second))
	require.Empty(t, recovered.Reason)
	require.Nil(t, recovered.InboundMbps, "the first good sample after a failure only sets the baseline")
	require.Nil(t, recovered.OutboundMbps)
	writeObservationNetDev(t, file, "eth0", 3_625_000, 7_250_000)
	measured := sampler.sampleTraffic(source, now.Add(40*time.Second))
	require.Equal(t, observationPtr(.1), measured.InboundMbps)
	require.Equal(t, observationPtr(.2), measured.OutboundMbps)

	var transitions []string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, "host traffic") {
			transitions = append(transitions, line)
		}
	}
	require.Len(t, transitions, 3, "only entering, changing and recovering are logged: %q", transitions)
	require.Contains(t, transitions[0], "host_counters_unreadable")
	require.Contains(t, transitions[1], "host_interface_missing")
	require.Contains(t, transitions[2], "recovered")
}

func TestObservationTrafficStaleClearsRatesWithoutTouchingCache(t *testing.T) {
	now := time.Unix(1800000000, 0)
	sampler := newObservationSampler(2)
	observation := emptyPerformanceObservation()
	observation.SampledAt = observationPtr(now.Unix())
	observation.Traffic = ObservationTraffic{Scope: "host_interface", Interface: "eth0", InboundMbps: observationPtr(1.5), OutboundMbps: observationPtr(7.)}
	sampler.record(observation)

	stale, err := sampler.read("1h", now.Add(observationMaxAge+time.Second))
	require.NoError(t, err)
	require.True(t, stale.Stale)
	require.Equal(t, ObservationTraffic{Scope: "host_interface", Interface: "eth0"}, stale.Traffic,
		"stale withdraws both rates and keeps the source; it is not reported as a host failure")

	fresh, err := sampler.read("1h", now)
	require.NoError(t, err)
	require.False(t, fresh.Stale)
	require.Equal(t, observation.Traffic, fresh.Traffic, "the stale projection did not rewrite the cached sample")
	point := fresh.Series.Points[len(fresh.Series.Points)-1]
	require.Equal(t, observationPtr(1.5), point.InboundMbps, "history keeps each direction")
	require.Equal(t, observationPtr(7.), point.OutboundMbps)
}

func TestObservationTrafficConcurrentSampleAndRead(t *testing.T) {
	sampler := newObservationSampler(8)
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				observation, err := sampler.read("1h", time.Now())
				if err != nil {
					t.Error(err)
					return
				}
				_ = newDashboardSystemStatus(observation, nil, DashboardSuccessRate{State: "no_requests"})
			}
		}()
	}
	for range 20 {
		sampler.sample(context.Background())
	}
	close(stop)
	readers.Wait()
	got, err := sampler.read("1h", time.Now())
	require.NoError(t, err)
	require.Equal(t, common.CurrentTrafficSource().Scope, got.Traffic.Scope)
	if got.Traffic.Reason == "" {
		require.NotNil(t, got.Traffic.InboundMbps, "back-to-back samples report a rate")
		require.NotNil(t, got.Traffic.OutboundMbps)
	}
}

func TestObservationHistoryWindowsGapsEvictionAndStale(t *testing.T) {
	previous := common.GetPerformanceMonitorConfig()
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true})
	t.Cleanup(func() { common.SetPerformanceMonitorConfig(previous) })
	now := time.Unix(1800000000, 0)
	sampler := newObservationSampler(3)
	for i := 0; i < 4; i++ {
		observation := emptyPerformanceObservation()
		observation.SampledAt = observationPtr(now.Add(time.Duration(i-3) * time.Minute).Unix())
		observation.Resources.CPU = ObservationResource{State: "ok", Available: true, Percent: observationPtr(float64(i + 1)), SampledAt: observation.SampledAt, Scope: "container"}
		observation.Storage.Database = ObservationProbe{Status: "ok", LatencyMs: observationPtr(1.)}
		log := ObservationProbe{Status: "ok", LatencyMs: observationPtr(1.)}
		observation.Storage.LogDatabase = &log
		observation.Process = &ObservationProcess{Goroutines: 3}
		sampler.record(observation)
	}
	for _, test := range []struct {
		window  string
		buckets int
		seconds int64
	}{{"1h", 61, 60}, {"6h", 73, 300}, {"24h", 97, 900}} {
		got, err := sampler.read(test.window, now)
		require.NoError(t, err)
		require.Len(t, got.Series.Points, test.buckets)
		require.Equal(t, test.seconds, got.Series.BucketSeconds)
		require.Equal(t, "instance", got.Scope)
		require.Nil(t, got.Series.Points[0].CPU, "no synthetic pre-start history")
	}
	got, err := sampler.read("1h", now)
	require.NoError(t, err)
	require.Nil(t, got.Series.Points[57].CPU, "ring evicts oldest sample")
	require.Equal(t, 2., *got.Series.Points[58].CPU)
	require.Equal(t, 4., *got.Series.Points[60].CPU)
	stale, err := sampler.read("1h", now.Add(31*time.Second))
	require.NoError(t, err)
	require.True(t, stale.Stale)
	require.Equal(t, "stale", stale.Status)
	require.Nil(t, stale.Resources.CPU.Percent)
	require.Nil(t, stale.Storage.Database.LatencyMs)
	require.Nil(t, stale.Process)
	require.Equal(t, "stale", stale.Storage.LogDatabase.Status)
	freshAgain, err := sampler.read("1h", now)
	require.NoError(t, err)
	require.Equal(t, "ok", freshAgain.Storage.LogDatabase.Status, "read projection must not mutate cached sample")
	_, err = sampler.read("7d", now)
	require.ErrorIs(t, err, ErrObservationWindow)
	unsampled, err := newObservationSampler(2).read("1h", now)
	require.NoError(t, err)
	require.Nil(t, unsampled.SampledAt)
	require.Nil(t, unsampled.Process)
	require.Equal(t, "unknown", unsampled.Status)
}

func TestObservationResourceExpiresBeforeObserverSnapshot(t *testing.T) {
	old := common.GetPerformanceMonitorConfig()
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true})
	t.Cleanup(func() { common.SetPerformanceMonitorConfig(old) })
	now := time.Unix(1800000000, 0)
	sampler := newObservationSampler(1)
	observation := emptyPerformanceObservation()
	observation.SampledAt = observationPtr(now.Unix())
	observation.Resources.CPU = ObservationResource{Available: true, State: "ok", Percent: observationPtr(12.), SampledAt: observationPtr(now.Add(-25 * time.Second).Unix()), Scope: "host"}
	sampler.record(observation)
	got, err := sampler.read("1h", now.Add(10*time.Second))
	require.NoError(t, err)
	require.False(t, got.Stale)
	require.Equal(t, "stale", got.Resources.CPU.State)
	require.Nil(t, got.Resources.CPU.Percent)
}

func TestObservationHealthUnknownDisabledFailureAndCoverage(t *testing.T) {
	observation := emptyPerformanceObservation()
	observation.Storage = ObservationStorage{Database: ObservationProbe{Status: "ok"}, LogShared: true, Redis: ObservationProbe{Status: "disabled", Reason: "disabled"}}
	observation.Resources.CPU = ObservationResource{State: "ok", Percent: observationPtr(91.), Threshold: 90}
	observation.Resources.Memory = ObservationResource{State: "ok", Percent: observationPtr(12.), Threshold: 90}
	observation.Resources.Disk.Threshold = 95
	checks, coverage, status := observationHealth(observation)
	require.Len(t, checks, 5, "shared log database is not checked twice")
	require.Equal(t, ObservationCoverage{Measured: 3, Total: 4}, coverage)
	require.Equal(t, "warning", status)
	require.Equal(t, "threshold_exceeded", checks[0].Reason)
	observation.Resources.CPU.Threshold = 95
	_, _, status = observationHealth(observation)
	require.Equal(t, "unknown", status, "missing disk cannot produce full health")
	observation.Storage.Database = ObservationProbe{Status: "critical", Reason: "timeout"}
	_, coverage, status = observationHealth(observation)
	require.Equal(t, "critical", status)
	require.Equal(t, 3, coverage.Measured, "an observed failure is still a measured check")
}

func TestObservationDisabledThresholdKeepsMeasurementAndExcludesCheck(t *testing.T) {
	previous := common.GetPerformanceMonitorConfig()
	t.Cleanup(func() { common.SetPerformanceMonitorConfig(previous) })
	now := time.Unix(1800000000, 0)
	sampler := newObservationSampler(1)
	observation := emptyPerformanceObservation()
	observation.SampledAt = observationPtr(now.Unix())
	observation.Resources.CPU = ObservationResource{State: "ok", Available: true, Percent: observationPtr(92.), Scope: "container", SampledAt: observation.SampledAt}
	observation.Storage = ObservationStorage{Database: ObservationProbe{Status: "ok"}, LogShared: true, Redis: ObservationProbe{Status: "disabled", Reason: "disabled"}}
	sampler.record(observation)
	for _, threshold := range []int{0, -1} {
		common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true, CPUThreshold: threshold})
		got, err := sampler.read("1h", now)
		require.NoError(t, err)
		require.True(t, got.Resources.CPU.Available)
		require.Equal(t, "ok", got.Resources.CPU.State)
		require.Equal(t, 92., *got.Resources.CPU.Percent)
		require.Equal(t, ObservationCheck{ID: "cpu", Status: "disabled", Reason: "disabled"}, got.Checks[0])
		require.Equal(t, ObservationCoverage{Measured: 1, Total: 1}, got.Coverage)
		require.Equal(t, "ok", got.Status, "disabled missing resource checks do not lower coverage or health")
	}
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90})
	got, err := sampler.read("1h", now)
	require.NoError(t, err)
	require.Equal(t, 90, got.Resources.CPU.Threshold)
	require.Equal(t, "warning", got.Status, "enabling a threshold applies to the same cached sample immediately")
	require.Equal(t, ObservationCoverage{Measured: 2, Total: 2}, got.Coverage)
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true, CPUThreshold: 99})
	got, err = sampler.read("1h", now)
	require.NoError(t, err)
	require.Equal(t, "ok", got.Status)
	require.Equal(t, 92., *got.Resources.CPU.Percent)
}

func TestObservationUnsampledEnabledThresholdRemainsUnknown(t *testing.T) {
	previous := common.GetPerformanceMonitorConfig()
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{Enabled: true, CPUThreshold: 90})
	t.Cleanup(func() { common.SetPerformanceMonitorConfig(previous) })
	got, err := newObservationSampler(1).read("1h", time.Now())
	require.NoError(t, err)
	require.Equal(t, ObservationCheck{ID: "cpu", Status: "unknown", Reason: "not_sampled"}, got.Checks[0])
	require.Equal(t, "unknown", got.Status)
	require.Equal(t, 0, got.Coverage.Measured)
	require.Equal(t, 3, got.Coverage.Total, "only CPU and two unsampled dependencies are enabled checks")
}

type observationTestConnection struct{ pings atomic.Int64 }

func (c *observationTestConnection) Ping(context.Context) error { c.pings.Add(1); return nil }
func (c *observationTestConnection) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unused")
}
func (c *observationTestConnection) Begin() (driver.Tx, error) { return nil, errors.New("unused") }
func (c *observationTestConnection) Close() error              { return nil }

type observationTestConnector struct{ connection *observationTestConnection }

func (c observationTestConnector) Connect(context.Context) (driver.Conn, error) {
	return c.connection, nil
}
func (c observationTestConnector) Driver() driver.Driver { return observationTestDriver(c) }

type observationTestDriver observationTestConnector

func (d observationTestDriver) Open(string) (driver.Conn, error) { return d.connection, nil }

func TestObservationStorageUsesSharedPoolAndReadDoesNotProbe(t *testing.T) {
	previousDB, previousLog, previousRedis := model.DB, model.LOG_DB, common.RedisEnabled
	connection := &observationTestConnection{}
	pool := sql.OpenDB(observationTestConnector{connection: connection})
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = previousDB, previousLog, previousRedis
		require.NoError(t, pool.Close())
	})
	model.DB = &gorm.DB{Config: &gorm.Config{ConnPool: pool}}
	model.LOG_DB = &gorm.DB{Config: &gorm.Config{ConnPool: pool}}
	common.RedisEnabled = false
	sampler := newObservationSampler(4)
	sampler.sample(context.Background())
	require.Equal(t, int64(1), connection.pings.Load(), "GORM sessions sharing a sql.DB are probed only once")
	for i := 0; i < 3; i++ {
		observation, err := sampler.read("1h", time.Now())
		require.NoError(t, err)
		require.True(t, observation.Storage.LogShared)
		require.Nil(t, observation.Storage.LogDatabase)
		require.Equal(t, "disabled", observation.Storage.Redis.Status)
		require.Nil(t, observation.Storage.Redis.LatencyMs)
	}
	require.Equal(t, int64(1), connection.pings.Load(), "polling never adds probes")
}

func TestObservationProbeTimeoutCancellationAndFailure(t *testing.T) {
	sampler := newObservationSampler(1)
	started := time.Now()
	probe := sampler.ping(context.Background(), "timeout", func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), observationProbeTimeout)
		<-ctx.Done()
		return ctx.Err()
	})
	require.Less(t, time.Since(started), 3*time.Second)
	require.Equal(t, "timeout", probe.Reason)
	require.Nil(t, probe.LatencyMs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	probe = sampler.ping(ctx, "canceled", func(ctx context.Context) error { return ctx.Err() })
	require.Equal(t, "canceled", probe.Reason)
	probe = sampler.ping(context.Background(), "failure", func(context.Context) error { return errors.New("unavailable") })
	require.Equal(t, "critical", probe.Status)
	require.Nil(t, probe.LatencyMs)
	probe = sampler.ping(context.Background(), "failure", func(context.Context) error { return nil })
	require.Equal(t, "ok", probe.Status)
	require.NotNil(t, probe.LatencyMs)
}

func TestObservationRedisFailureIsNotZeroOrHealthy(t *testing.T) {
	previousDB, previousLog, previousRDB, previousEnabled := model.DB, model.LOG_DB, common.RDB, common.RedisEnabled
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB, common.RedisEnabled = client, true
	model.DB, model.LOG_DB = nil, nil
	t.Cleanup(func() {
		require.NoError(t, client.Close())
		model.DB, model.LOG_DB, common.RDB, common.RedisEnabled = previousDB, previousLog, previousRDB, previousEnabled
	})
	sampler := newObservationSampler(1)
	storage := sampler.probeStorage(context.Background())
	require.Equal(t, "ok", storage.Redis.Status)
	require.NotNil(t, storage.Redis.LatencyMs)
	require.Equal(t, "critical", storage.Database.Status)
	server.SetError("LOADING unavailable")
	storage = sampler.probeStorage(context.Background())
	require.Equal(t, "critical", storage.Redis.Status)
	require.Equal(t, "unavailable", storage.Redis.Reason)
	require.Nil(t, storage.Redis.LatencyMs)
}

func TestObservationSamplesAndAggregatesGatewayInFlight(t *testing.T) {
	previous := gatewayConcurrency
	gatewayConcurrency = NewConcurrencyManager()
	t.Cleanup(func() { gatewayConcurrency = previous })

	pUser, err := AcquireUserConcurrency(context.Background(), 7, 10)
	require.NoError(t, err)
	defer pUser.Release()
	pChan1, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11, MaxConcurrency: 10})
	require.NoError(t, err)
	defer pChan1.Release()
	pChan2, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11, MaxConcurrency: 10})
	require.NoError(t, err)
	defer pChan2.Release()

	sampler := newObservationSampler(4)
	sampler.sample(context.Background())
	// Read with the sample's own timestamp so a minute rollover between sample()
	// and read() can never move the point into the previous bucket.
	sampler.mu.RLock()
	require.NotNil(t, sampler.latest)
	require.NotNil(t, sampler.latest.SampledAt)
	sampledAt := time.Unix(*sampler.latest.SampledAt, 0)
	sampler.mu.RUnlock()

	got, err := sampler.read("1h", sampledAt)
	require.NoError(t, err)
	last := got.Series.Points[len(got.Series.Points)-1]
	require.NotNil(t, last.UserInFlight)
	require.NotNil(t, last.ChannelInFlight)
	require.Equal(t, 1.0, *last.UserInFlight)
	require.Equal(t, 2.0, *last.ChannelInFlight)
	require.Nil(t, got.Series.Points[0].UserInFlight, "unsampled history stays null")

	// Stale read preserves timestamped history without mutating the cached ring.
	stale, err := sampler.read("1h", sampledAt.Add(observationMaxAge+time.Second))
	require.NoError(t, err)
	require.True(t, stale.Stale)
	gotAgain, err := sampler.read("1h", sampledAt)
	require.NoError(t, err)
	require.False(t, gotAgain.Stale)
	require.Equal(t, 1.0, *gotAgain.Series.Points[len(gotAgain.Series.Points)-1].UserInFlight)

	// Releasing permits drops O(1) totals cleanly to real zero.
	pUser.Release()
	pChan1.Release()
	pChan2.Release()
	uTotal, cTotal := gatewayInFlightTotals()
	require.Equal(t, 0, uTotal)
	require.Equal(t, 0, cTotal)

	now := time.Unix(1800000030, 0)
	series := buildObservationSeries([]ObservationSeriesPoint{
		{Ts: now.Unix() - 20, UserInFlight: observationPtr(2.), ChannelInFlight: observationPtr(4.)},
		{Ts: now.Unix() - 10, UserInFlight: observationPtr(4.), ChannelInFlight: observationPtr(6.)},
	}, "1h", now.Unix()-3600, now.Unix(), 60)
	bucket := series.Points[len(series.Points)-1]
	require.NotNil(t, bucket.UserInFlight)
	require.NotNil(t, bucket.ChannelInFlight)
	require.Equal(t, 3.0, *bucket.UserInFlight)
	require.Equal(t, 5.0, *bucket.ChannelInFlight)

	// Concurrent acquire/release beside sampler.sample() must not deadlock or go negative.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 1; i <= 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				p, acqErr := AcquireUserConcurrency(context.Background(), id, 10)
				if acqErr == nil {
					p.Release()
				}
			}
		}(i)
	}
	for range 8 {
		sampler.sample(context.Background())
	}
	close(stop)
	wg.Wait()
	uFinal, cFinal := gatewayInFlightTotals()
	require.Equal(t, 0, uFinal)
	require.Equal(t, 0, cFinal)
}
