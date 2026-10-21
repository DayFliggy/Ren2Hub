package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// DeleteDisabledChannelIDs 在 MySQL、PostgreSQL 上带 FOR UPDATE，SQLite 上不加锁；三种方言都要
// 返回实际删除的 ID 并删掉对应能力行。
func TestDeleteDisabledChannelIDsDatabases(t *testing.T) {
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
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("chdel_%d_", time.Now().UnixNano())}, Logger: logger.Default.LogMode(logger.Silent)})
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
				require.NoError(t, db.Migrator().DropTable(&Ability{}, &Channel{}))
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
			for id, status := range map[int]int{11: common.ChannelStatusManuallyDisabled, 12: common.ChannelStatusAutoDisabled, 13: common.ChannelStatusEnabled} {
				require.NoError(t, db.Create(&Channel{Id: id, Type: constant.ChannelTypeOpenAI, Status: status, Name: "delete-disabled", Key: "k", Models: "m"}).Error)
				require.NoError(t, db.Create(&Ability{Model: "m", ChannelId: id, Enabled: true}).Error)
			}
			ids, err := DeleteDisabledChannelIDs()
			require.NoError(t, err)
			require.ElementsMatch(t, []int{11, 12}, ids)
			var channels, abilities []int
			require.NoError(t, db.Model(&Channel{}).Order("id").Pluck("id", &channels).Error)
			require.NoError(t, db.Model(&Ability{}).Order("channel_id").Pluck("channel_id", &abilities).Error)
			require.Equal(t, []int{13}, channels)
			require.Equal(t, []int{13}, abilities)
		})
	}
}
