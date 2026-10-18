package model

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// External targets must be freshly created, isolated acceptance databases.
// Never reset a populated target, including when a previous test run failed.
func TestCoreEmptyDatabaseInitialization(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			envName := "CORE_STARTUP_MYSQL_DSN"
			if dialect == "postgres" {
				envName = "CORE_STARTUP_POSTGRES_DSN"
			}
			if dialect == "sqlite" {
				envName = "CORE_STARTUP_SQLITE_DSN"
				t.Setenv(envName, "local")
				previous := common.SQLitePath
				common.SQLitePath = filepath.Join(t.TempDir(), "fresh.db")
				t.Cleanup(func() { common.SQLitePath = previous })
			} else if os.Getenv(envName) == "" {
				t.Skip(envName + " is not configured")
			}
			db, dbType, err := chooseDB(envName, false)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			tables, err := db.Migrator().GetTables()
			require.NoError(t, err)
			require.Empty(t, tables, "acceptance target is not empty; refusing to modify it")
			previousDB, previousLog := DB, LOG_DB
			previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(dbType, dbType)
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(previousType, previousLogType)
				initCol()
			})
			require.NoError(t, migrateDB())
			require.NoError(t, migrateLOGDB())
			for _, table := range []string{"users", "tokens", "channels", "top_ups", "tasks", "task_plugins", "tickets", "ticket_messages", "ticket_attachments", "token_route_profiles", "token_route_entries", "gmpay_orders", "gmpay_merchant_versions"} {
				assert.True(t, db.Migrator().HasTable(table), table)
			}
			for _, table := range []string{"billing_schemas", "billing_events", "subscriptions", "subscription_plans", "subscription_orders", "user_subscriptions", "system_instances"} {
				assert.False(t, db.Migrator().HasTable(table), table)
			}
			for _, table := range []string{"users", "tokens", "channels", "abilities"} {
				assert.False(t, db.Migrator().HasColumn(table, "group"), table)
			}
			for _, table := range []string{"affiliate_records", "admin_quota_operations"} {
				assert.True(t, db.Migrator().HasTable(table), table)
			}
			user := User{Username: "restart", Quota: 987654, Setting: `{"language":"zh-CN"}`}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, migrateDB())
			require.NoError(t, migrateLOGDB())
			var after User
			require.NoError(t, db.First(&after, user.Id).Error)
			assert.Equal(t, user.Quota, after.Quota)
			assert.Equal(t, user.Setting, after.Setting)
			tablesBefore, err := db.Migrator().GetTables()
			require.NoError(t, err)
			require.NoError(t, db.Exec("CREATE TABLE billing_schemas (id INTEGER PRIMARY KEY, version INTEGER)").Error)
			require.NoError(t, db.Exec("INSERT INTO billing_schemas (id, version) VALUES (1, 5)").Error)
			require.ErrorContains(t, migrateDB(), "separate empty database")
			tablesAfter, err := db.Migrator().GetTables()
			require.NoError(t, err)
			assert.Len(t, tablesAfter, len(tablesBefore)+1)
			require.NoError(t, db.First(&after, user.Id).Error)
			assert.Equal(t, user.Quota, after.Quota)
		})
	}
}
