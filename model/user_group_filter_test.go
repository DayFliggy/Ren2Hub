package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The account group column is the reserved word "group"; the filter and the
// per-group counts must quote it correctly on every supported database.
func TestUserGroupFilterAndCountsAcrossDatabases(t *testing.T) {
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "TEST_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN"}} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " is not configured; database contract is unverified")
			}
			db, err := gatewayStartupDatabase(t, database.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousKind, previousLogKind := DB, common.MainDatabaseType(), common.LogDatabaseType()
			DB = db
			common.SetDatabaseTypes(database.kind, database.kind)
			initCol()
			t.Cleanup(func() {
				DB = previousDB
				common.SetDatabaseTypes(previousKind, previousLogKind)
				initCol()
				connection, err := db.DB()
				require.NoError(t, err)
				assert.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}))
			require.NoError(t, db.Create(&[]User{
				{Username: "group-default-a", Password: "password", AffCode: "group-default-a", Group: "default"},
				{Username: "group-default-b", Password: "password", AffCode: "group-default-b", Group: "default"},
				{Username: "group-premium", Password: "password", AffCode: "group-premium", Group: "premium"},
			}).Error)
			// GORM's default tag fills a zero Group; store the ungrouped row explicitly.
			ungrouped := User{Username: "group-none", Password: "password", AffCode: "group-none"}
			require.NoError(t, db.Create(&ungrouped).Error)
			require.NoError(t, db.Model(&ungrouped).Update("group", "").Error)

			counts, err := CountUsersByGroup(db.Model(&User{}))
			require.NoError(t, err)
			assert.Equal(t, map[string]int{"default": 2, "premium": 1}, counts)

			var premium []User
			require.NoError(t, WhereUserGroup(db.Model(&User{}), "premium").Find(&premium).Error)
			require.Len(t, premium, 1)
			assert.Equal(t, "group-premium", premium[0].Username)

			var missing int64
			require.NoError(t, WhereUserGroup(db.Model(&User{}), "vip").Count(&missing).Error)
			assert.Zero(t, missing)
		})
	}
}
