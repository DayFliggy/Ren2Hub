package perfmetrics

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQueryOverviewMergesStoredAndHotBucketsIntoOneZeroFilledSeries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "perf.db")), &gorm.Config{})
	require.NoError(t, err)
	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		hotBuckets.Range(func(key, _ any) bool { hotBuckets.Delete(key); return true })
		sqlDB, _ := db.DB()
		assert.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}, &model.PerfMetricFlushReceipt{}))

	current := bucketStart(time.Now().Unix())
	older := current - 3*3600
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "model-a", Group: "default", BucketTs: older, RequestCount: 4, SuccessCount: 4, TotalLatencyMs: 400, OutputTokens: 100, GenerationMs: 1000},
		{ModelName: "model-b", Group: "vip", BucketTs: older, RequestCount: 1, SuccessCount: 0, TotalLatencyMs: 900},
		// Outside the window: must not reach the summary.
		{ModelName: "model-a", Group: "default", BucketTs: current - 48*3600, RequestCount: 50, SuccessCount: 0},
	}).Error)
	Record(Sample{Model: "model-b", Group: "default", LatencyMs: 300, Success: true})
	Record(Sample{Model: "model-b", Group: "default", LatencyMs: 100, Success: false})

	result, err := QueryOverview(6)
	require.NoError(t, err)
	assert.True(t, result.Enabled)
	assert.Equal(t, int64(3600), result.BucketSeconds)
	assert.Equal(t, int64(7), result.Summary.Requests)
	require.NotNil(t, result.Summary.SuccessRate)
	assert.InDelta(t, 71.43, *result.Summary.SuccessRate, 0.01)

	// 6 hours of hourly buckets from an aligned start: 6 or 7 points depending on alignment.
	assert.GreaterOrEqual(t, len(result.Series), 6)
	assert.LessOrEqual(t, len(result.Series), 7)
	points := map[int64]OverviewPoint{}
	for index, point := range result.Series {
		if index > 0 {
			assert.Equal(t, result.Series[index-1].Ts+3600, point.Ts, "series must be contiguous")
		}
		points[point.Ts] = point
	}
	require.Contains(t, points, older)
	assert.Equal(t, int64(5), points[older].Requests)
	assert.Equal(t, int64(260), points[older].AvgLatencyMs)
	require.NotNil(t, points[older].SuccessRate)
	assert.InDelta(t, 80.0, *points[older].SuccessRate, 0.001)
	require.Contains(t, points, current, "unflushed traffic must be visible before the next flush")
	assert.Equal(t, int64(2), points[current].Requests)
	empty := points[older+3600]
	assert.Zero(t, empty.Requests)
	assert.Nil(t, empty.SuccessRate, "an idle hour has no success rate rather than 0%")

	require.Len(t, result.TopModels, 2)
	assert.Equal(t, "model-a", result.TopModels[0].ModelName, "ties break by name")
	assert.Equal(t, int64(4), result.TopModels[0].Requests)
	assert.Equal(t, "model-b", result.TopModels[1].ModelName)
	require.NotNil(t, result.TopModels[1].SuccessRate)
	assert.InDelta(t, 33.33, *result.TopModels[1].SuccessRate, 0.01)
}
