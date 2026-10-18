package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestRedemptionSortingDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			dbType := common.DatabaseTypeSQLite
			if dialect != "sqlite" {
				key := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
				if dialect == "postgres" {
					key = "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
				}
				dsn := os.Getenv(key)
				if dsn == "" {
					t.Skip(key + " is not configured; isolated database required")
				}
				driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
				if dialect == "postgres" {
					driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
				}
			}
			db, err := gorm.Open(driver, &gorm.Config{
				NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("redeemsort_%d_", time.Now().UnixNano())},
				Logger:         logger.Default.LogMode(logger.Silent),
			})
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				connection.SetMaxOpenConns(1)
			}
			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(dbType)
			t.Cleanup(func() {
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				require.NoError(t, db.Migrator().DropTable(&Redemption{}))
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&Redemption{}))
			rows := []Redemption{
				{Id: 1, Name: "sort-one", Key: "one", Status: common.RedemptionCodeStatusUsed, CreatedTime: 300, RedeemedTime: 100},
				{Id: 2, Name: "sort-two", Key: "two", Status: common.RedemptionCodeStatusUsed, CreatedTime: 100, RedeemedTime: 300},
				{Id: 3, Name: "sort-three", Key: "three", Status: common.RedemptionCodeStatusUsed, CreatedTime: 100, RedeemedTime: 300},
				{Id: 4, Name: "other", Key: "four", Status: common.RedemptionCodeStatusDisabled, CreatedTime: 200},
			}
			require.NoError(t, db.Create(&rows).Error)
			for _, test := range []struct {
				field, order string
				ids          []int
			}{
				{"id", "asc", []int{1, 2, 3}}, {"id", "desc", []int{3, 2, 1}},
				{"created_time", "asc", []int{2, 3, 1}}, {"created_time", "desc", []int{1, 3, 2}},
				{"used_time", "asc", []int{1, 2, 3}}, {"used_time", "desc", []int{3, 2, 1}},
			} {
				t.Run(test.field+"/"+test.order, func(t *testing.T) {
					sort, err := ParseRedemptionSort(test.field, test.order)
					require.NoError(t, err)
					var ids []int
					for offset := range 3 {
						page, total, err := SearchRedemptions("sort-", "3", offset, 1, sort)
						require.NoError(t, err)
						require.EqualValues(t, 3, total)
						require.Len(t, page, 1)
						ids = append(ids, page[0].Id)
					}
					require.Equal(t, test.ids, ids)
				})
			}
		})
	}
}
