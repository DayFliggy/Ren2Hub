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
// Exercise retained admission, wallet and terminal-state contracts on every dialect.
func TestTaskPollingContractDatabaseMatrix(t *testing.T) {
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
				&model.TopUpBonusUsageHourly{},
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
			t.Run("admission_failures_do_not_penalize_provider", TestTaskPollAdmissionFailuresAreNotUpstreamFailures)
			t.Run("admission_deletion_has_no_side_effects", TestTaskPollAdmissionDeletionSkipsWithoutSideEffects)
			// Terminal settlement, failure refunds and timeout refunds are task
			// billing: an earlier synchronous usage S stays the only bonus-window row.
			synced := model.TopUpBonusUsageHourly{UserID: 990001, HourUTC: 1_790_000_000 / 3600 * 3600, Quota: 4242}
			require.NoError(t, db.Create(&synced).Error)
			t.Run("timeout_refund_is_once", TestSweepTimedOutTasksAt72HourBoundaryAndRefundsOnce)
			t.Run("terminal_usage_settlement", TestUpdateBatchTasksSettlesTieredUsageForTerminalStates)
			t.Run("terminal_failure_refund", TestUpdateBatchTasksRefundsFailedTaskWithoutUsageSettlement)
			var bonusRows []model.TopUpBonusUsageHourly
			require.NoError(t, db.Find(&bonusRows).Error)
			assert.Equal(t, []model.TopUpBonusUsageHourly{synced}, bonusRows)
		})
	}
}
