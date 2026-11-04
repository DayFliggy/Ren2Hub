package model

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLegacyBillingDatabaseRejectedBeforeMigration(t *testing.T) {
	for _, table := range []string{"billing_schemas", "billing_events"} {
		t.Run(table, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, db.Exec("CREATE TABLE "+table+" (id INTEGER PRIMARY KEY, version INTEGER)").Error)
			require.NoError(t, db.Exec("INSERT INTO "+table+" (id, version) VALUES (1, 5)").Error)
			previous := DB
			DB = db
			t.Cleanup(func() { DB = previous })
			err = migrateDB()
			require.ErrorContains(t, err, "separate empty database")
			tables, err := db.Migrator().GetTables()
			require.NoError(t, err)
			assert.Equal(t, []string{table}, tables)
			var version int
			require.NoError(t, db.Raw("SELECT version FROM "+table+" WHERE id = 1").Scan(&version).Error)
			assert.Equal(t, 5, version)
		})
	}
}

func TestGroupedDatabaseRejectedEvenWhenEmpty(t *testing.T) {
	for _, table := range []string{"users", "tokens", "channels", "abilities"} {
		t.Run(table, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "old.db")), &gorm.Config{})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			require.NoError(t, db.Exec("CREATE TABLE "+table+" (id INTEGER PRIMARY KEY, `group` TEXT)").Error)
			require.ErrorContains(t, rejectLegacyBillingDatabase(db), "separate empty database")
			tables, err := db.Migrator().GetTables()
			require.NoError(t, err)
			require.Equal(t, []string{table}, tables)
		})
	}
}

func TestUpstreamSyncRootInitializationPropagatesDatabaseFailure(t *testing.T) {
	for _, stage := range []string{"read", "create"} {
		t.Run(stage, func(t *testing.T) {
			db := useGmpayTestDB(t)
			outage := errors.New("root database unavailable")
			createAttempts := 0
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("upstream-sync:root-create", func(tx *gorm.DB) {
				if tx.Statement.Table == "users" {
					createAttempts++
					if stage == "create" {
						tx.AddError(outage)
					}
				}
			}))
			if stage == "read" {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("upstream-sync:root-read", func(tx *gorm.DB) {
					if tx.Statement.Table == "users" {
						tx.AddError(outage)
					}
				}))
			}
			assert.ErrorIs(t, createRootAccountIfNeed(), outage, "a database outage must not be acknowledged as successful initialization")
			if stage == "read" {
				assert.Zero(t, createAttempts, "a failed query is not evidence that the installation has no users")
			}
		})
	}
}
