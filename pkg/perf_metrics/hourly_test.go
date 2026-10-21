package perfmetrics

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQueryHourlyAllSumsModelsPerHour(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "perf.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	previous := model.DB
	model.DB = db
	hotBuckets.Clear()
	t.Cleanup(func() { model.DB = previous; hotBuckets.Clear(); require.NoError(t, sqlDB.Close()) })

	now := time.Now()
	start, end := queryWindow(now, 3)
	current := end - end%3600
	first := current - 2*3600
	require.Equal(t, first, start)
	for _, row := range []model.PerfMetric{
		{ModelName: "a", BucketTs: first, RequestCount: 3, SuccessCount: 3, TotalLatencyMs: 300, OutputTokens: 30, GenerationMs: 3000},
		// 5 分钟桶也归入所在小时。
		{ModelName: "b", BucketTs: first + 300, RequestCount: 1, SuccessCount: 0, TotalLatencyMs: 900},
		{ModelName: "a", BucketTs: first - 3600, RequestCount: 50, SuccessCount: 50},
	} {
		require.NoError(t, model.UpsertPerfMetric(&row))
	}
	hot := &atomicBucket{}
	hot.add(Sample{Model: "c", LatencyMs: 100, Success: true})
	hotBuckets.Store(bucketKey{model: "c", bucketTs: current}, hot)

	result, err := QueryHourlyAll(3)
	require.NoError(t, err)
	require.Equal(t, []int64{first, first + 3600, current}, []int64{result.Series[0].Ts, result.Series[1].Ts, result.Series[2].Ts})
	require.Len(t, result.Series, 3)

	hour := result.Series[0]
	require.EqualValues(t, 4, hour.RequestCount)
	require.EqualValues(t, 3, hour.SuccessCount)
	require.Equal(t, 75.0, *hour.SuccessRate)
	require.EqualValues(t, 300, *hour.AvgLatencyMs)
	require.Equal(t, 10.0, *hour.AvgTps)

	empty := result.Series[1]
	require.Zero(t, empty.RequestCount)
	require.Nil(t, empty.SuccessRate)
	require.Nil(t, empty.AvgLatencyMs)
	require.Nil(t, empty.AvgTps)

	require.EqualValues(t, 1, result.Series[2].RequestCount, "unflushed hot buckets are included")
	require.Nil(t, result.Series[2].AvgTps)
	require.NotNil(t, result.Summary)
	require.Equal(t, 80.0, result.Summary.SuccessRate)
	require.EqualValues(t, 260, result.Summary.AvgLatencyMs)
}
