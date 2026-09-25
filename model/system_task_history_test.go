package model

import (
	"os"
	"testing"

	"github.com/glebarez/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestSystemTaskHistoryDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var dialector gorm.Dialector
			switch dialect {
			case "sqlite":
				dialector = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				dialector = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				dialector = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialector, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "history_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			previousDB := DB
			DB = db
			t.Cleanup(func() {
				DB = previousDB
				require.NoError(t, db.Migrator().DropTable(&SystemTask{}))
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&SystemTask{}))
			var version string
			versionQuery := "SELECT version()"
			if dialect == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)

			tasks := []SystemTask{
				{TaskID: "model-failed-old", Type: SystemTaskTypeModelUpdate, Status: SystemTaskStatusFailed},
				{TaskID: "model-success", Type: SystemTaskTypeModelUpdate, Status: SystemTaskStatusSucceeded},
				{TaskID: "channel-failed", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusFailed},
				{TaskID: "model-failed-recent", Type: SystemTaskTypeModelUpdate, Status: SystemTaskStatusFailed},
				{TaskID: "model-latest", Type: SystemTaskTypeModelUpdate, Status: SystemTaskStatusSucceeded},
				{TaskID: "channel-pending", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusPending},
				{TaskID: "channel-running", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusRunning},
				{TaskID: "log-only-history", Type: SystemTaskTypeLogCleanup, Status: SystemTaskStatusSucceeded},
			}
			require.NoError(t, db.Create(&tasks).Error)
			filter := SystemTaskFilter{Scope: "history", Type: SystemTaskTypeModelUpdate, Status: SystemTaskStatusFailed}
			page, total, err := ListSystemTasks(filter, 1, 1)
			require.NoError(t, err)
			assert.EqualValues(t, 2, total)
			require.Len(t, page, 1)
			assert.Equal(t, "model-failed-old", page[0].TaskID)
			page, total, err = ListSystemTasks(filter, 2, 1)
			require.NoError(t, err)
			assert.EqualValues(t, 2, total)
			assert.Empty(t, page)

			active, total, err := ListSystemTasks(SystemTaskFilter{Scope: "active"}, 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 2, total)
			require.Len(t, active, 2)
			assert.Equal(t, "channel-running", active[0].TaskID)
			assert.Equal(t, "channel-pending", active[1].TaskID)

			deleted, err := DeleteSystemTaskHistory(filter)
			require.NoError(t, err)
			assert.EqualValues(t, 2, deleted)
			_, total, err = ListSystemTasks(SystemTaskFilter{Scope: "history"}, 0, 20)
			require.NoError(t, err)
			assert.EqualValues(t, 4, total, "other types and statuses are untouched")

			deleted, err = DeleteSystemTaskHistory(SystemTaskFilter{Scope: "active", Status: SystemTaskStatusRunning})
			require.NoError(t, err)
			assert.Zero(t, deleted, "caller cannot override the terminal-status restriction")
			deleted, err = DeleteSystemTaskHistory(SystemTaskFilter{})
			require.NoError(t, err)
			assert.EqualValues(t, 2, deleted)
			remaining, total, err := ListSystemTasks(SystemTaskFilter{}, 0, 100)
			require.NoError(t, err)
			assert.EqualValues(t, 4, total)
			var remainingIDs []string
			for _, task := range remaining {
				remainingIDs = append(remainingIDs, task.TaskID)
			}
			assert.ElementsMatch(t, []string{"model-latest", "channel-pending", "channel-running", "log-only-history"}, remainingIDs)
			latest, err := GetLatestSystemTasks([]string{SystemTaskTypeModelUpdate, SystemTaskTypeChannelTest, SystemTaskTypeLogCleanup})
			require.NoError(t, err)
			assert.Equal(t, "model-latest", latest[SystemTaskTypeModelUpdate].TaskID)
			assert.Equal(t, "channel-running", latest[SystemTaskTypeChannelTest].TaskID)
			assert.Equal(t, "log-only-history", latest[SystemTaskTypeLogCleanup].TaskID)
			deleted, err = DeleteSystemTaskHistory(SystemTaskFilter{})
			require.NoError(t, err)
			assert.Zero(t, deleted, "repeated cleanup preserves the scheduler's latest runs")
		})
	}
}
