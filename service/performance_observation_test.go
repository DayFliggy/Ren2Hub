package service

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
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

func TestObservationTrafficFirstResetClockGapAndIdle(t *testing.T) {
	sampler := trafficSampler{}
	now := time.Unix(1800000000, 0)
	first := sampler.sample(now, 100, 200)
	require.Nil(t, first.RequestMbps)
	require.Nil(t, first.ResponseMbps)
	measured := sampler.sample(now.Add(10*time.Second), 1_250_100, 2_500_200)
	require.Equal(t, 1., *measured.RequestMbps)
	require.Equal(t, 2., *measured.ResponseMbps)
	idle := sampler.sample(now.Add(20*time.Second), 1_250_100, 2_500_200)
	require.Zero(t, *idle.RequestMbps)
	require.Nil(t, sampler.sample(now.Add(30*time.Second), 1, 1).RequestMbps, "counter reset")
	require.Nil(t, sampler.sample(now.Add(30*time.Second), 2, 2).RequestMbps, "same timestamp")
	require.Nil(t, sampler.sample(now.Add(29*time.Second), 3, 3).ResponseMbps, "clock reversal")
	require.Nil(t, sampler.sample(now.Add(80*time.Second), 4, 4).ResponseMbps, "gap is not a fresh instantaneous sample")
	require.NotNil(t, sampler.sample(now.Add(90*time.Second), 5, 5).RequestMbps)
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
