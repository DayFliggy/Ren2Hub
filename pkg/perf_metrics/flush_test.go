package perfmetrics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMetricFlush(t *testing.T) {
	t.Helper()
	saved := model.DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.FlushOperation{}, &model.PerfMetric{}))
	model.DB = db
	hotBuckets = sync.Map{}
	pendingFlush = map[bucketKey]*metricFlush{}
	t.Cleanup(func() {
		model.DB = saved
		hotBuckets = sync.Map{}
		pendingFlush = map[bucketKey]*metricFlush{}
		require.NoError(t, sqlDB.Close())
	})
}

func TestFlushIncludesCurrentBucketAndRetriesWithoutDuplicating(t *testing.T) {
	setupMetricFlush(t)
	Record(Sample{Model: "model", Success: true, LatencyMs: 50, HasTtft: true, TtftMs: 10, OutputTokens: 4, GenerationMs: 40})
	injected := errors.New("metric storage unavailable")
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("fail_metric", func(tx *gorm.DB) {
		if tx.Statement.Table == "perf_metrics" {
			tx.AddError(injected)
		}
	}))
	require.ErrorIs(t, Flush(context.Background()), injected)
	Record(Sample{Model: "model", LatencyMs: 20})
	require.NoError(t, model.DB.Callback().Create().Remove("fail_metric"))
	require.NoError(t, Flush(context.Background()))
	require.NoError(t, Flush(context.Background()))
	var rows []model.PerfMetric
	require.NoError(t, model.DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].RequestCount)
	assert.Equal(t, int64(1), rows[0].SuccessCount)
	assert.Equal(t, int64(70), rows[0].TotalLatencyMs)
	assert.Equal(t, int64(10), rows[0].TtftSumMs)
	assert.Equal(t, int64(4), rows[0].OutputTokens)
	assert.Equal(t, int64(40), rows[0].GenerationMs)
}

func TestQueryResolvesLostFlushAcknowledgementWithoutDoubleCounting(t *testing.T) {
	setupMetricFlush(t)
	ts := bucketStart(time.Now().Unix())
	key := bucketKey{model: "model", bucketTs: ts}
	metric := &model.PerfMetric{ModelName: "model", BucketTs: ts, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 50}
	require.NoError(t, model.FlushPerfMetric(context.Background(), "lost-ack", metric))
	pendingFlush[key] = &metricFlush{id: "lost-ack", metric: metric}
	Record(Sample{Model: "model", Success: true, LatencyMs: 30})
	result, err := QuerySummaryAll(1)
	require.NoError(t, err)
	require.Len(t, result.Models, 1)
	assert.Equal(t, int64(2), result.Models[0].RequestCount)
	assert.Equal(t, int64(40), result.Models[0].AvgLatencyMs)
	require.NoError(t, Flush(context.Background()))
	var rows []model.PerfMetric
	require.NoError(t, model.DB.Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2), rows[0].RequestCount)
}
