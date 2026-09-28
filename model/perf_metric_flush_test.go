package model

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var errMetricCommitAcknowledgement = errors.New("metric commit acknowledgement lost")

type metricAcknowledgementPool struct {
	gorm.ConnPool
	lose atomic.Bool
}

func (pool *metricAcknowledgementPool) BeginTx(ctx context.Context, options *sql.TxOptions) (gorm.ConnPool, error) {
	tx, err := pool.ConnPool.(gorm.TxBeginner).BeginTx(ctx, options)
	if err != nil {
		return nil, err
	}
	return &metricAcknowledgementTx{Tx: tx, lose: &pool.lose}, nil
}

type metricAcknowledgementTx struct {
	*sql.Tx
	lose *atomic.Bool
}

func (tx *metricAcknowledgementTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.lose.CompareAndSwap(true, false) {
		return errMetricCommitAcknowledgement
	}
	return nil
}

func TestPerfMetricFlushReceiptAcrossDatabases(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&PerfMetricFlushReceipt{}))
		metric := PerfMetric{ModelName: "flush", Group: "default", BucketTs: 3600, RequestCount: 2, SuccessCount: 1, TotalLatencyMs: 300, OutputTokens: 10, GenerationMs: 200}
		original := db.Statement.ConnPool
		basePool := original
		if prepared, ok := original.(*gorm.PreparedStmtDB); ok {
			basePool = prepared.ConnPool
		}
		pool := &metricAcknowledgementPool{ConnPool: basePool}
		pool.lose.Store(true)
		db.Statement.ConnPool = pool
		t.Cleanup(func() { db.Statement.ConnPool = original })
		require.ErrorIs(t, FlushPerfMetric(context.Background(), "lost-ack", &metric), errMetricCommitAcknowledgement)
		var row PerfMetric
		require.NoError(t, db.First(&row).Error)
		assert.EqualValues(t, 2, row.RequestCount, "the first transaction actually committed")
		var wait sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wait.Add(1)
			go func() { defer wait.Done(); results <- FlushPerfMetric(context.Background(), "lost-ack", &metric) }()
		}
		wait.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		require.NoError(t, db.First(&row).Error)
		assert.EqualValues(t, 2, row.RequestCount)
		assert.EqualValues(t, 300, row.TotalLatencyMs)
		changed := metric
		changed.RequestCount++
		require.ErrorContains(t, FlushPerfMetric(context.Background(), "lost-ack", &changed), "identity conflict")
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("metric_receipt_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "perf_metric_flush_receipts" {
				tx.AddError(assert.AnError)
			}
		}))
		require.ErrorIs(t, FlushPerfMetric(context.Background(), "rolled-back", &metric), assert.AnError)
		require.NoError(t, db.Callback().Update().Remove("metric_receipt_failure"))
		require.NoError(t, db.First(&row).Error)
		assert.EqualValues(t, 2, row.RequestCount, "receipt and projection roll back together")
		require.NoError(t, FlushPerfMetric(context.Background(), "rolled-back", &metric))
		require.NoError(t, db.First(&row).Error)
		assert.EqualValues(t, 4, row.RequestCount)
	})
}

func TestPerfMetricReceiptMigrationPreservesBillingFormat(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&BillingSchema{}))
		require.NoError(t, db.Create(&BillingSchema{ID: 1, Version: BillingSchemaVersion}).Error)
		metric := PerfMetric{ModelName: "existing", Group: "default", BucketTs: 3600, RequestCount: 7}
		require.NoError(t, db.Create(&metric).Error)
		for range 2 {
			require.NoError(t, prepareBillingSchema(db))
			require.NoError(t, db.AutoMigrate(&PerfMetricFlushReceipt{}))
			require.NoError(t, requireBillingSchema(db))
		}
		var marker BillingSchema
		require.NoError(t, db.First(&marker, 1).Error)
		assert.Equal(t, 4, marker.Version)
		require.NoError(t, db.First(&metric, metric.Id).Error)
		assert.EqualValues(t, 7, metric.RequestCount)
	})
}
