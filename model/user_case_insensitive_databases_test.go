package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestCheckUserExistOrDeletedCaseInsensitiveDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			dbType := common.DatabaseTypeSQLite
			if dialect != "sqlite" {
				key := "TEST_MYSQL_DSN"
				fallback := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
				if dialect == "postgres" {
					key, fallback = "TEST_POSTGRES_DSN", "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
				}
				dsn := os.Getenv(key)
				if dsn == "" {
					dsn = os.Getenv(fallback)
				}
				if dsn == "" {
					t.Skip(key + " is not configured; isolated database required")
				}
				driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
				if dialect == "postgres" {
					driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
				}
			}

			prefix := fmt.Sprintf("userci_%d_", time.Now().UnixNano())
			db, err := gorm.Open(driver, &gorm.Config{
				Logger:         logger.Default.LogMode(logger.Silent),
				NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
			})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() {
				_ = db.Migrator().DropTable(&User{})
				_ = sqlDB.Close()
			})
			require.NoError(t, db.AutoMigrate(&User{}))

			prevDB := DB
			prevType := common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(dbType)
			t.Cleanup(func() {
				DB = prevDB
				common.SetMainDatabaseType(prevType)
			})

			active := &User{
				Username: "AliceCase",
				Password: "password-hash",
				Email:    "AliceCase@Example.com",
				Status:   common.UserStatusEnabled,
				AffCode:  "aff-active",
			}
			require.NoError(t, db.Create(active).Error)

			deleted := &User{
				Username: "DeletedCase",
				Password: "password-hash",
				Email:    "DeletedCase@Example.com",
				Status:   common.UserStatusEnabled,
				AffCode:  "aff-deleted",
			}
			require.NoError(t, db.Create(deleted).Error)
			require.NoError(t, db.Delete(deleted).Error)

			for _, tc := range []struct {
				name     string
				username string
				email    string
				want     bool
			}{
				{"active lowercase", "alicecase", "", true},
				{"active uppercase", "ALICECASE", "", true},
				{"active with unrelated email", "alicecase", "other@example.com", true},
				{"deleted mixed case", "dElEtEdCaSe", "", true},
				{"deleted with unrelated email", "DELETEDCASE", "other@example.com", true},
				{"distinct username matched by email case-insensitively", "brandnew", "ALICECASE@example.com", true},
				{"unrelated username and email", "brandnew", "brandnew@example.com", false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					exist, err := CheckUserExistOrDeleted(tc.username, tc.email)
					require.NoError(t, err)
					assert.Equal(t, tc.want, exist)
				})
			}
		})
	}
}
