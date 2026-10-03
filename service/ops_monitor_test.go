package service

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func opsTestValue(value float64) *float64 { return &value }

func TestDeriveOpsChecksGradesEachSignalAgainstItsRule(t *testing.T) {
	healthy := OpsStorage{Database: OpsDatabaseProbe{Status: OpsStatusOK}, Redis: OpsRedisProbe{Status: OpsStatusUnknown}}
	resources := OpsResources{
		MonitorEnabled: true,
		CPU:            OpsPercent{Percent: opsTestValue(95.5), Threshold: 90},
		Memory:         OpsUsage{Percent: opsTestValue(85), Threshold: 90},
		Disk:           OpsUsage{Percent: opsTestValue(99), Threshold: 0},
	}
	gateway := OpsGateway{SuccessRate: opsTestValue(98.5), RejectedPerMinute: opsTestValue(0)}
	checks := map[string]OpsCheck{}
	for _, check := range deriveOpsChecks(resources, gateway, healthy, true) {
		checks[check.Key] = check
	}
	assert.Equal(t, OpsStatusCritical, checks["cpu"].Status, "above the 503 threshold")
	assert.Equal(t, OpsStatusWarning, checks["memory"].Status, "within the warning margin")
	assert.Equal(t, OpsStatusOK, checks["disk"].Status, "threshold 0 disables the limit")
	require.NotNil(t, checks["disk"].Threshold)
	assert.Zero(t, *checks["disk"].Threshold)
	assert.Equal(t, OpsStatusWarning, checks["success_rate"].Status, "client errors keep the rate a warning")
	assert.Equal(t, OpsStatusOK, checks["admission"].Status)
	assert.Equal(t, OpsStatusOK, checks["master"].Status)
	assert.NotContains(t, checks, "redis", "disabled Redis is not graded")
	assert.NotContains(t, checks, "log_database", "a shared log database is not graded twice")

	resources.MonitorEnabled = false
	gateway = OpsGateway{Admission: AdmissionMetrics{Draining: true}, RejectedPerMinute: opsTestValue(2)}
	storage := OpsStorage{
		Database:    OpsDatabaseProbe{Status: OpsStatusOK},
		LogDatabase: &OpsDatabaseProbe{Status: OpsStatusCritical, Error: "timeout"},
		Redis:       OpsRedisProbe{Enabled: true, Status: OpsStatusCritical, Error: "unavailable"},
	}
	checks = map[string]OpsCheck{}
	for _, check := range deriveOpsChecks(resources, gateway, storage, false) {
		checks[check.Key] = check
	}
	assert.Equal(t, OpsStatusUnknown, checks["cpu"].Status, "a disabled monitor is not a healthy reading")
	assert.Equal(t, OpsStatusUnknown, checks["success_rate"].Status)
	assert.Equal(t, OpsStatusWarning, checks["admission"].Status)
	assert.Equal(t, OpsStatusWarning, checks["draining"].Status)
	assert.Equal(t, OpsStatusWarning, checks["master"].Status, "a slave runs no scheduled tasks")
	assert.Equal(t, OpsStatusWarning, checks["log_database"].Status, "logs wait in the outbox")
	assert.Equal(t, "timeout", checks["log_database"].Error)
	assert.Equal(t, OpsStatusCritical, checks["redis"].Status)
}

func TestRollupOpsStatusReportsTheWorstMeasuredCheck(t *testing.T) {
	assert.Equal(t, OpsStatusUnknown, rollupOpsStatus(nil))
	assert.Equal(t, OpsStatusUnknown, rollupOpsStatus([]OpsCheck{{Status: OpsStatusUnknown}}))
	assert.Equal(t, OpsStatusOK, rollupOpsStatus([]OpsCheck{{Status: OpsStatusUnknown}, {Status: OpsStatusOK}}))
	assert.Equal(t, OpsStatusCritical, rollupOpsStatus([]OpsCheck{{Status: OpsStatusWarning}, {Status: OpsStatusCritical}, {Status: OpsStatusOK}}))
}

func TestOpsHealthScoreIsDerivedFromTheServerSideChecks(t *testing.T) {
	assert.Nil(t, opsHealthScore(OpsStatusUnknown, []OpsCheck{{Status: OpsStatusUnknown}}))
	assert.Equal(t, 100, *opsHealthScore(OpsStatusOK, []OpsCheck{{Status: OpsStatusOK}}))
	// A warning roll-up has no critical check, by rollupOpsStatus.
	assert.Equal(t, 85, *opsHealthScore(OpsStatusWarning, []OpsCheck{
		{Status: OpsStatusOK}, {Status: OpsStatusWarning},
	}))
	assert.Equal(t, 15, *opsHealthScore(OpsStatusCritical, []OpsCheck{
		{Status: OpsStatusCritical}, {Status: OpsStatusCritical}, {Status: OpsStatusWarning},
	}))
}

func TestBuildOpsSeriesAveragesGaugesAndDerivesRatesAcrossGaps(t *testing.T) {
	nan := math.NaN()
	points := []opsPoint{
		// Before the window: seeds the first bucket's baseline only.
		{ts: 990, cpu: 99, memory: nan, disk: nan, netUp: nan, netDown: nan, requests: 100, successes: 100, granted: 50},
		{ts: 1000, cpu: 10, memory: nan, disk: nan, netUp: nan, netDown: nan, requests: 110, successes: 109, granted: 60},
		{ts: 1010, cpu: nan, memory: nan, disk: nan, netUp: nan, netDown: nan, requests: 120, successes: 118, granted: 70},
		// Bucket 1020 has no samples; it must stay a gap.
		{ts: 1040, cpu: 30, memory: nan, disk: nan, netUp: nan, netDown: nan, requests: 120, successes: 118, granted: 70, rejected: 3},
		// A counter reset (for example a replaced manager) yields no rate.
		{ts: 1060, cpu: 40, memory: nan, disk: nan, netUp: nan, netDown: nan, requests: 5, successes: 5, granted: 1},
	}
	series := buildOpsSeries(points, 1000, "1h", 20)
	assert.Equal(t, "1h", series.Window)
	require.Len(t, series.Points, 3)

	first := series.Points[0]
	assert.Equal(t, int64(1000), first.Ts)
	require.NotNil(t, first.CPU)
	assert.InDelta(t, 10, *first.CPU, 0.001, "NaN samples are skipped, not averaged as zero")
	assert.Nil(t, first.Memory)
	require.NotNil(t, first.RequestsPerMinute)
	assert.InDelta(t, 60, *first.RequestsPerMinute, 0.001, "20 requests over the 20 s since the baseline")
	require.NotNil(t, first.SuccessRate)
	assert.InDelta(t, 90, *first.SuccessRate, 0.001)

	idle := series.Points[1]
	assert.Equal(t, int64(1040), idle.Ts)
	require.NotNil(t, idle.RequestsPerMinute)
	assert.Zero(t, *idle.RequestsPerMinute)
	assert.Nil(t, idle.SuccessRate, "no requests means no success rate")
	require.NotNil(t, idle.RejectedPerMinute)
	assert.InDelta(t, 6, *idle.RejectedPerMinute, 0.001)

	reset := series.Points[2]
	assert.Nil(t, reset.RequestsPerMinute)
	assert.Nil(t, reset.GrantedPerMinute)
	assert.Nil(t, reset.SuccessRate)

	empty := buildOpsSeries(nil, 1000, "6h", 120)
	assert.NotNil(t, empty.Points, "an empty series encodes as [] rather than null")
}

func TestOpsSamplerReadsChronologicallyAcrossRingWrap(t *testing.T) {
	sampler := newOpsSampler(3)
	latest, points := sampler.read(0)
	assert.Nil(t, latest)
	assert.Nil(t, points)
	for ts := int64(1); ts <= 5; ts++ {
		sampler.record(opsPoint{ts: ts * 30, granted: uint64(ts)}, opsSnapshot{sampledAt: ts * 30})
	}
	latest, points = sampler.read(0)
	require.NotNil(t, latest)
	assert.Equal(t, int64(150), latest.sampledAt)
	require.Len(t, points, 3)
	assert.Equal(t, []int64{90, 120, 150}, []int64{points[0].ts, points[1].ts, points[2].ts})
	_, points = sampler.read(100)
	assert.Len(t, points, 2)

	granted, rejected := opsRecentRates(points)
	assert.Nil(t, granted, "samples 30 s apart cannot yield a per-minute rate yet")
	assert.Nil(t, rejected)
	_, points = sampler.read(0)
	granted, _ = opsRecentRates(points)
	require.NotNil(t, granted)
	assert.InDelta(t, 2, *granted, 0.001)
}

func TestReadOpsOverviewServesTheLatestSampleForAValidWindow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "overview.db")), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogDB, previousMonitor := model.DB, model.LOG_DB, opsMonitor
	previousConfig, previousMaster, previousRedis := common.GetPerformanceMonitorConfig(), common.IsMasterNode, common.RedisEnabled
	model.DB, model.LOG_DB, opsMonitor = db, db, newOpsSampler(16)
	// Pin every graded input: host readings are unknown when the monitor is off,
	// Redis is not graded, and this node is master. Only the database is measured.
	common.SetPerformanceMonitorConfig(common.PerformanceMonitorConfig{})
	common.IsMasterNode, common.RedisEnabled = true, false
	t.Cleanup(func() {
		model.DB, model.LOG_DB, opsMonitor = previousDB, previousLogDB, previousMonitor
		common.SetPerformanceMonitorConfig(previousConfig)
		common.IsMasterNode, common.RedisEnabled = previousMaster, previousRedis
		sqlDB, _ := db.DB()
		assert.NoError(t, sqlDB.Close())
	})

	_, err = ReadOpsOverview("1h")
	require.ErrorIs(t, err, errOpsNotSampled, "no sample must not read as an idle, healthy instance")
	_, err = ReadOpsOverview("7d")
	require.ErrorIs(t, err, ErrOpsWindowInvalid)

	opsMonitor.record(opsMonitor.sample(context.Background()))
	overview, err := ReadOpsOverview("1h")
	require.NoError(t, err)
	assert.Equal(t, opsSchemaVersion, overview.SchemaVersion)
	assert.Equal(t, OpsStatusOK, overview.Storage.Database.Status)
	assert.Equal(t, OpsStatusOK, overview.Status)
	require.NotNil(t, overview.HealthScore)
	assert.Equal(t, 100, *overview.HealthScore)
	assert.Nil(t, overview.Storage.LogDatabase, "a shared log database is reported once")
	require.Len(t, overview.Series.Points, 1)
	require.Len(t, overview.Gateway.WaitBucketBoundsMS, 8)
	assert.Nil(t, overview.Gateway.WaitBucketBoundsMS[7], "the unbounded bucket is null")
	assert.Equal(t, int64(5000), *overview.Gateway.WaitBucketBoundsMS[6])
	assert.Nil(t, overview.Gateway.GrantedPerMinute, "one sample cannot yield a rate")
	encoded, err := common.Marshal(overview)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"log_database":null`)
}

func TestSystemTaskHealthPrefersRunningThenInactiveFailingStaleIdle(t *testing.T) {
	now := common.StartTime + 3600
	scheduled := SystemTaskTypeSummary{Scheduled: true, Enabled: true, IntervalSeconds: 60}
	for _, tc := range []struct {
		name    string
		summary SystemTaskTypeSummary
		run     model.SystemTaskActivity
		want    string
	}{
		{"an active run wins", scheduled, model.SystemTaskActivity{Active: true, LastFailureAt: now}, "running"},
		{"disabled or no pending work", SystemTaskTypeSummary{Scheduled: true}, model.SystemTaskActivity{LastFailureAt: now}, "inactive"},
		{"failure after success", scheduled, model.SystemTaskActivity{LastSuccessAt: now - 30, LastFailureAt: now - 10}, "failing"},
		{"two missed runs plus slack", scheduled, model.SystemTaskActivity{LastSuccessAt: now - 181}, "stale"},
		{"never ran since start", scheduled, model.SystemTaskActivity{}, "stale"},
		{"recent success", scheduled, model.SystemTaskActivity{LastSuccessAt: now - 179}, "ok"},
		{"on-demand task never run", SystemTaskTypeSummary{Enabled: true}, model.SystemTaskActivity{}, "idle"},
	} {
		assert.Equal(t, tc.want, systemTaskHealth(tc.summary, tc.run, now), tc.name)
	}
}

func TestOpsProbesReportCategoriesWithoutDriverDetails(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ops.db")), &gorm.Config{})
	require.NoError(t, err)
	sampler := newOpsSampler(1)
	probe := sampler.probeDatabase(context.Background(), "database", db, common.DatabaseTypeSQLite)
	assert.Equal(t, OpsStatusOK, probe.Status)
	assert.Equal(t, "sqlite", probe.Kind)
	require.NotNil(t, probe.LatencyMs)
	assert.Empty(t, probe.Error)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	probe = sampler.probeDatabase(context.Background(), "database", db, common.DatabaseTypeSQLite)
	assert.Equal(t, OpsStatusCritical, probe.Status)
	assert.Nil(t, probe.LatencyMs, "a failed ping has no latency")
	assert.Equal(t, "unavailable", probe.Error)
	assert.True(t, sampler.failing["database"])

	missing := sampler.probeDatabase(context.Background(), "log_database", nil, common.DatabaseTypeSQLite)
	assert.Equal(t, OpsStatusCritical, missing.Status)

	previous := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = previous })
	redis := sampler.probeRedis(context.Background())
	assert.False(t, redis.Enabled)
	assert.Equal(t, OpsStatusUnknown, redis.Status)
}

func TestSystemTaskSummaryOrdersOutcomesThatFinishInTheSameSecond(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "task-summary.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}))
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		sqlDB, err := db.DB()
		require.NoError(t, err)
		assert.NoError(t, sqlDB.Close())
	})
	withSystemTaskRegistry(t, logCleanupHandler{})

	for _, tc := range []struct {
		name     string
		statuses []model.SystemTaskStatus
		want     string
	}{
		{"success then failure", []model.SystemTaskStatus{model.SystemTaskStatusSucceeded, model.SystemTaskStatusFailed}, "failing"},
		{"failure then success", []model.SystemTaskStatus{model.SystemTaskStatusFailed, model.SystemTaskStatusSucceeded}, "ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.Where("type = ?", model.SystemTaskTypeLogCleanup).Delete(&model.SystemTask{}).Error)
			// On-demand tasks can finish and be started again within a second;
			// UpdatedAt intentionally preserves the model's second precision.
			for _, status := range tc.statuses {
				require.NoError(t, db.Create(&model.SystemTask{
					TaskID: string(status), Type: model.SystemTaskTypeLogCleanup, Status: status, UpdatedAt: 100,
				}).Error)
			}
			summaries, err := SummarizeSystemTasks(time.Unix(101, 0))
			require.NoError(t, err)
			require.Len(t, summaries, 1)
			assert.Equal(t, tc.want, summaries[0].Status)
		})
	}
}
