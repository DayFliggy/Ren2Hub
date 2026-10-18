package model

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestChannelMetricsDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			dbType := common.DatabaseTypeSQLite
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_PERF_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_PERF_MYSQL_DSN is not configured")
				}
				driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
			case "postgres":
				dsn := os.Getenv("TEST_PERF_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_PERF_POSTGRES_DSN is not configured")
				}
				driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			require.False(t, db.Migrator().HasTable(&Log{}), "isolated metrics target already contains logs; refusing reset")
			require.NoError(t, db.AutoMigrate(&Log{}))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Log{})) })
			previous, previousType, enabled := LOG_DB, common.LogDatabaseType(), common.LogConsumeEnabled
			LOG_DB = db
			common.SetLogDatabaseType(dbType)
			common.LogConsumeEnabled = true
			t.Cleanup(func() { LOG_DB = previous; common.SetLogDatabaseType(previousType); common.LogConsumeEnabled = enabled })
			now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			end, start := now.Unix()+1, now.Unix()+1-86400
			logs := []Log{
				{UserId: 11, Type: LogTypeConsume, CreatedAt: start, ModelName: "model-one", Quota: 7, PromptTokens: 2, CompletionTokens: 3, UseTime: 4, Other: `{"cache_tokens":0}`},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: start + 3599, ModelName: "model-one", Quota: 11, PromptTokens: 5, CompletionTokens: 7, UseTime: 8, Other: `{"cache_tokens":5,"cache_creation_tokens":9}`},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: start + 3600, ModelName: "model-two", Quota: 13, PromptTokens: 9, CompletionTokens: 11},
				{UserId: 12, Type: LogTypeConsume, CreatedAt: start, Quota: 100, Other: `{"cache_tokens":100}`},
				{UserId: 11, Type: LogTypeError, CreatedAt: start, Quota: 100},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: end, Quota: 100},
			}
			require.NoError(t, db.Create(&logs).Error)
			rows, err := ReadDashboardUsage(context.Background(), 11, start, end)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			require.EqualValues(t, 0, rows[0].Bucket)
			require.EqualValues(t, 2, rows[0].Requests)
			require.EqualValues(t, 18, rows[0].Quota)
			require.EqualValues(t, 7, rows[0].Input)
			require.EqualValues(t, 10, rows[0].Output)
			require.EqualValues(t, 1, rows[1].Bucket)
			cache, err := ReadDashboardCacheUsage(context.Background(), 11, start, end)
			require.NoError(t, err)
			var read, write, known int64
			for _, row := range cache {
				read += row.Read
				write += row.Write
				known += row.ReadRecords
			}
			require.EqualValues(t, 5, read)
			require.EqualValues(t, 9, write)
			require.EqualValues(t, 2, known)
			home, err := GetHomeRequestMetricsWithContext(context.Background(), now)
			require.NoError(t, err)
			require.EqualValues(t, 4, *home.Requests24h)
			require.Len(t, home.Buckets, 24)
			require.EqualValues(t, 3, home.HourlyRequests[0])
			require.EqualValues(t, 1, home.HourlyRequests[1])
		})
	}
}
