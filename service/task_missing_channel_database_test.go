package service

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// DSNs must point at empty, isolated databases owned by the database runner.
// Reuse the polling cases so all dialects protect the same CAS and money rules.
func TestTaskPollingRefundDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct {
		name common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "TEST_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN"},
	} {
		t.Run(string(dialect.name), func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			if dialect.env != "" {
				dsn := os.Getenv(dialect.env)
				if dsn == "" {
					t.Skip(dialect.env + " is not configured")
				}
				if dialect.name == common.DatabaseTypeMySQL {
					driver = mysql.Open(dsn)
				} else {
					driver = postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true})
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })

			oldDB, oldLogDB := model.DB, model.LOG_DB
			oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
			oldCache, oldRedis := common.MemoryCacheEnabled, common.RedisEnabled
			oldBatch, oldLogs, oldExport := common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
			model.DB, model.LOG_DB = db, db
			common.SetDatabaseTypes(dialect.name, dialect.name)
			common.RedisEnabled, common.BatchUpdateEnabled = false, false
			common.LogConsumeEnabled, common.DataExportEnabled = true, false
			t.Cleanup(func() {
				model.DB, model.LOG_DB = oldDB, oldLogDB
				common.SetDatabaseTypes(oldMainType, oldLogType)
				common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
				common.LogConsumeEnabled, common.DataExportEnabled = oldLogs, oldExport
				common.MemoryCacheEnabled = true
				model.InitChannelCache()
				common.MemoryCacheEnabled = oldCache
			})
			tables := []any{
				&model.Task{}, &model.User{}, &model.Token{}, &model.Log{}, &model.Channel{},
				&model.Midjourney{}, &model.TopUp{}, &model.SystemTask{}, &model.SystemTaskLock{},
				&model.UsageDiscountHourly{},
			}
			t.Cleanup(func() { assert.NoError(t, db.Migrator().DropTable(tables...)) })
			require.NoError(t, db.AutoMigrate(tables...))
			common.MemoryCacheEnabled = true
			model.InitChannelCache()
			common.MemoryCacheEnabled = false

			versionQuery := "select version()"
			if dialect.name == common.DatabaseTypeSQLite {
				versionQuery = "select sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database: %s", version)
			t.Run("missing_channel_refunds_preconsumed", func(t *testing.T) {
				TestUpdateVideoTasksRefundsPreConsumedWhenChannelMissing(t)
				requireTaskPollingRefundLogs(t, model.Log{UserId: 21, ChannelId: 21, TokenId: 21, Quota: 5000})
			})
			t.Run("cas_refunds_once_and_keeps_legacy_rule", func(t *testing.T) {
				TestUpdateBatchTasksMissingChannelRefundsOnceAndKeepsLegacyRule(t)
				requireTaskPollingRefundLogs(t, model.Log{UserId: 22, ChannelId: 22, Quota: 1200})
			})
			t.Run("channel_read_error_keeps_reservation", func(t *testing.T) {
				TestUpdateBatchTasksKeepsTasksWhenChannelReadFails(t)
				requireTaskPollingRefundLogs(t)
			})
			t.Run("cache_miss_keeps_reservation", func(t *testing.T) {
				TestUpdateVideoTasksKeepsTasksWhenOnlyTheCacheMissesTheChannel(t)
				requireTaskPollingRefundLogs(t)
			})
			t.Run("duplicate_upstream_id_uses_own_channel", TestRunTaskPollingOnceHandlesOneTaskPerUpstreamID)
		})
	}
}

func requireTaskPollingRefundLogs(t *testing.T, expected ...model.Log) {
	t.Helper()
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Order("id").Find(&logs).Error)
	require.Len(t, logs, len(expected))
	for i, want := range expected {
		assert.Equal(t, model.LogTypeRefund, logs[i].Type)
		assert.Equal(t, want.UserId, logs[i].UserId)
		assert.Equal(t, want.ChannelId, logs[i].ChannelId)
		assert.Equal(t, want.TokenId, logs[i].TokenId)
		assert.Equal(t, want.Quota, logs[i].Quota)
	}
}
