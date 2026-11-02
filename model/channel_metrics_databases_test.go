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
			require.False(t, db.Migrator().HasTable(&QuotaData{}), "isolated metrics target already contains quota_data; refusing reset")
			require.NoError(t, db.AutoMigrate(&Log{}, &QuotaData{}))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Log{}, &QuotaData{})) })
			previous, previousMain, previousType, enabled := LOG_DB, DB, common.LogDatabaseType(), common.LogConsumeEnabled
			LOG_DB, DB = db, db
			common.SetLogDatabaseType(dbType)
			common.LogConsumeEnabled = true
			t.Cleanup(func() {
				LOG_DB, DB = previous, previousMain
				common.SetLogDatabaseType(previousType)
				common.LogConsumeEnabled = enabled
			})
			now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			end, start := now.Unix()+1, now.Unix()+1-86400
			logs := []Log{
				{UserId: 11, Type: LogTypeConsume, CreatedAt: start, ModelName: "model-one", Quota: 7, UseTime: 4, RequestId: "ok-1"},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: start + 3599, ModelName: "model-one", Quota: 11, UseTime: 8, RequestId: "retried"},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: start + 3600, ModelName: "model-two", Quota: 13, RequestId: "ok-2"},
				{UserId: 12, Type: LogTypeConsume, CreatedAt: start, Quota: 100, UseTime: 90, RequestId: "other-user"},
				{UserId: 11, Type: LogTypeError, CreatedAt: start, RequestId: "retried"},
				{UserId: 11, Type: LogTypeError, CreatedAt: start + 10, RequestId: "failed"},
				{UserId: 11, Type: LogTypeError, CreatedAt: start + 20, RequestId: "failed"},
				{UserId: 11, Type: LogTypeError, CreatedAt: end - 1, RequestId: "late-retry"},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: end, Quota: 100, RequestId: "late-retry"},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: end + 1, RequestId: "outside-window"},
				{UserId: 11, Type: LogTypeConsume, CreatedAt: end + 2, RequestId: "late-retry"},
				{UserId: 12, Type: LogTypeConsume, CreatedAt: end + 2, RequestId: "failed"},
				{UserId: 12, Type: LogTypeError, CreatedAt: start, RequestId: "other-failed"},
			}
			require.NoError(t, db.Create(&logs).Error)
			hour := now.Unix() - now.Unix()%3600
			quota := []QuotaData{
				{UserID: 11, ModelName: "model-one", CreatedAt: hour - 3600, Count: 2, Quota: 18, TokenUsed: 17, CacheReadTokens: 5, CacheCreationTokens: 9, ChannelID: 1},
				{UserID: 11, ModelName: "model-one", CreatedAt: hour - 3600, Count: 1, Quota: 3, TokenUsed: 4, CacheReadTokens: 1, ChannelID: 2},
				{UserID: 11, ModelName: "model-two", CreatedAt: hour, Count: 1, Quota: 13, TokenUsed: 20},
				{UserID: 12, ModelName: "model-one", CreatedAt: hour, Count: 9, Quota: 900, TokenUsed: 900},
				{UserID: 11, ModelName: "model-one", CreatedAt: hour + 3600, Count: 9, Quota: 900, TokenUsed: 900},
			}
			require.NoError(t, db.Create(&quota).Error)
			rows, err := ReadDashboardQuota(context.Background(), DashboardUserScope(11), start, end)
			require.NoError(t, err)
			require.Len(t, rows, 2)
			require.Equal(t, DashboardQuotaRow{CreatedAt: hour - 3600, ModelName: "model-one", Requests: 3, Quota: 21, Tokens: 21, CacheReadTokens: 6, CacheCreationTokens: 9}, rows[0])
			require.Equal(t, DashboardQuotaRow{CreatedAt: hour, ModelName: "model-two", Requests: 1, Quota: 13, Tokens: 20}, rows[1])
			latency, err := ReadDashboardLatency(context.Background(), DashboardUserScope(11), start, end)
			require.NoError(t, err)
			require.ElementsMatch(t, []DashboardLatencyRow{{ModelName: "model-one", Requests: 2, UseTime: 12}, {ModelName: "model-two", Requests: 1}}, latency)
			outcome, err := ReadDashboardOutcome(context.Background(), DashboardUserScope(11), start, end)
			require.NoError(t, err)
			// late-retry 只成功一次；其他区间和其他用户的成功不能改变本区间结果。
			require.Equal(t, DashboardOutcome{Succeeded: 4, Failed: 1}, outcome)
			home, err := GetHomeRequestMetricsWithContext(context.Background(), now)
			require.NoError(t, err)
			require.EqualValues(t, 4, *home.Requests24h)
			require.Len(t, home.Buckets, 24)
			require.EqualValues(t, 3, home.HourlyRequests[0])
			require.EqualValues(t, 1, home.HourlyRequests[1])

			// 全站读取不连接 users，保留已删除账号留下的历史桶和日志。
			siteRows, err := ReadDashboardQuota(context.Background(), DashboardSiteScope(), start, end)
			require.NoError(t, err)
			require.Len(t, siteRows, 3)
			require.Equal(t, DashboardQuotaRow{CreatedAt: hour, ModelName: "model-one", Requests: 9, Quota: 900, Tokens: 900}, siteRows[1])
			siteLatency, err := ReadDashboardLatency(context.Background(), DashboardSiteScope(), start, end)
			require.NoError(t, err)
			require.ElementsMatch(t, append(latency, DashboardLatencyRow{Requests: 1, UseTime: 90}), siteLatency)
			siteOutcome, err := ReadDashboardOutcome(context.Background(), DashboardSiteScope(), start, end)
			require.NoError(t, err)
			require.Equal(t, DashboardOutcome{Succeeded: 5, Failed: 2}, siteOutcome)

			// 同一 request_id 的两个用户各算一次，另一个用户的成功不掩盖失败。
			require.NoError(t, db.Create(&[]Log{
				{UserId: 13, Type: LogTypeConsume, CreatedAt: start, RequestId: "ok-1"},
				{UserId: 12, Type: LogTypeError, CreatedAt: start, RequestId: "ok-1"},
				{UserId: 12, Type: LogTypeError, CreatedAt: start + 1, RequestId: "ok-1"},
			}).Error)
			siteOutcome, err = ReadDashboardOutcome(context.Background(), DashboardSiteScope(), start, end)
			require.NoError(t, err)
			require.Equal(t, DashboardOutcome{Succeeded: 6, Failed: 3}, siteOutcome)
			personal, err := ReadDashboardOutcome(context.Background(), DashboardUserScope(12), start, end)
			require.NoError(t, err)
			require.Equal(t, DashboardOutcome{Succeeded: 1, Failed: 2}, personal)
		})
	}
}
