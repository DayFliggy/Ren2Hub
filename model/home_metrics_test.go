package model

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestHomeMetricsExactRollingBuckets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "metrics.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	previous, enabled, dbType := LOG_DB, common.LogConsumeEnabled, common.LogDatabaseType()
	LOG_DB = db
	common.LogConsumeEnabled = true
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() { LOG_DB = previous; common.LogConsumeEnabled = enabled; common.SetLogDatabaseType(dbType) })
	require.NoError(t, db.AutoMigrate(&Log{}))
	now := time.Date(2026, 10, 18, 12, 37, 20, 0, time.UTC)
	end := now.Unix() + 1
	start := end - 86400
	for _, at := range []int64{start - 1, start, start + 3599, start + 3600, end - 1, end} {
		require.NoError(t, db.Create(&Log{Type: LogTypeConsume, CreatedAt: at}).Error)
	}
	require.NoError(t, db.Create(&Log{Type: LogTypeError, CreatedAt: start}).Error)
	result, err := GetHomeRequestMetricsWithContext(context.Background(), now)
	require.NoError(t, err)
	require.EqualValues(t, 4, *result.Requests24h)
	require.Len(t, result.Buckets, 24)
	require.EqualValues(t, 2, result.HourlyRequests[0])
	require.EqualValues(t, 1, result.HourlyRequests[1])
	require.EqualValues(t, 1, result.HourlyRequests[23])
	require.Equal(t, start, result.Buckets[0].Start)
	require.Equal(t, end, result.Buckets[23].End)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = GetHomeRequestMetricsWithContext(canceled, now)
	require.ErrorIs(t, err, context.Canceled)
	common.LogConsumeEnabled = false
	result, err = GetHomeRequestMetricsWithContext(context.Background(), now)
	require.NoError(t, err)
	require.False(t, result.Available)
	require.Nil(t, result.Requests24h)
	common.LogConsumeEnabled = true
	require.NoError(t, db.Migrator().DropTable(&Log{}))
	_, err = GetHomeRequestMetricsWithContext(context.Background(), now)
	require.Error(t, err)
}
