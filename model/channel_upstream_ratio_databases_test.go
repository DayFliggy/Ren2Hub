package model

import (
	"errors"
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

// 上游倍率的 CAS 写回、孤儿清理和到期查询在三种方言上都要成立。
func TestChannelUpstreamRatioDatabases(t *testing.T) {
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
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("upr_%d_", time.Now().UnixNano())}, Logger: logger.Default.LogMode(logger.Silent)})
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
				require.NoError(t, db.Migrator().DropTable(&ChannelUpstreamRatio{}, &Channel{}))
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelUpstreamRatio{}))
			for id, channelType := range map[int]int{1: constant.ChannelTypeSub2API, 2: constant.ChannelTypeNewAPI, 3: constant.ChannelTypeOpenAI} {
				require.NoError(t, db.Create(&Channel{Id: id, Type: channelType, Name: "upstream-ratio", Key: "k", Models: "m"}).Error)
			}

			due, err := GetDueUpstreamRatioChannels(100, 20)
			require.NoError(t, err)
			require.Len(t, due, 2, "type 59/60 without records are due; mainstream types wait for manual detection")

			record := &ChannelUpstreamRatio{ChannelId: 1, Source: UpstreamRatioSourceSub2API, Mode: UpstreamRatioModeAuto, Status: UpstreamRatioStatusOK, Ratio: "1.2", NextProbeAt: 200}
			require.NoError(t, SaveChannelUpstreamRatio(record, 0, nil))
			require.EqualValues(t, 1, record.Version)
			duplicate := &ChannelUpstreamRatio{ChannelId: 1, Mode: UpstreamRatioModeAuto}
			require.ErrorIs(t, SaveChannelUpstreamRatio(duplicate, 0, nil), ErrUpstreamRatioConflict)

			record.Ratio = "1.3"
			require.NoError(t, SaveChannelUpstreamRatio(record, 1, nil))
			stale := *record
			stale.Ratio = "9"
			require.ErrorIs(t, SaveChannelUpstreamRatio(&stale, 1, nil), ErrUpstreamRatioConflict)
			rejected := errors.New("channel changed")
			require.ErrorIs(t, SaveChannelUpstreamRatio(record, 2, func(*Channel) error { return rejected }), rejected)
			stored, err := GetChannelUpstreamRatio(1)
			require.NoError(t, err)
			require.Equal(t, "1.3", stored.Ratio)
			require.EqualValues(t, 2, stored.Version)

			due, err = GetDueUpstreamRatioChannels(100, 20)
			require.NoError(t, err)
			require.Len(t, due, 1)
			due, err = GetDueUpstreamRatioChannels(300, 20)
			require.NoError(t, err)
			require.Len(t, due, 2)

			require.NoError(t, db.Create(&ChannelUpstreamRatio{ChannelId: 99, Mode: UpstreamRatioModeAuto, Version: 1}).Error)
			removed, err := DeleteOrphanChannelUpstreamRatios()
			require.NoError(t, err)
			require.EqualValues(t, 1, removed)
			missing, err := GetChannelUpstreamRatio(99)
			require.NoError(t, err)
			require.Nil(t, missing)
			require.Error(t, SaveChannelUpstreamRatio(&ChannelUpstreamRatio{ChannelId: 98}, 0, nil), "deleted channels cannot get a record")

			// 渠道 2 删除后用同一 ID 重建：旧一代的记录是孤儿，版本 0 写入替换它，调度清理也会删除它。
			require.NoError(t, SaveChannelUpstreamRatio(&ChannelUpstreamRatio{ChannelId: 2, Mode: UpstreamRatioModeManual, Ratio: "0.5"}, 0, nil))
			require.NoError(t, db.Model(&Channel{}).Where("id = ?", 2).Update("created_time", 5000).Error)
			orphaned, err := HasOrphanChannelUpstreamRatios()
			require.NoError(t, err)
			require.True(t, orphaned, "a record from the previous generation is an orphan")
			replacement := &ChannelUpstreamRatio{ChannelId: 2, Mode: UpstreamRatioModeAuto, ChannelCreatedTime: 5000}
			require.NoError(t, SaveChannelUpstreamRatio(replacement, 0, nil))
			require.EqualValues(t, 5000, replacement.ChannelCreatedTime)
			orphaned, err = HasOrphanChannelUpstreamRatios()
			require.NoError(t, err)
			require.False(t, orphaned)
			require.NoError(t, db.Model(&Channel{}).Where("id = ?", 2).Update("created_time", 6000).Error)
			removed, err = DeleteOrphanChannelUpstreamRatios()
			require.NoError(t, err)
			require.EqualValues(t, 1, removed)
			current, err := GetChannelUpstreamRatio(1)
			require.NoError(t, err)
			require.NotNil(t, current, "records of unchanged channels survive the sweep")

			// 旧一代渠道的在途写回与新一代记录版本相同，也必须失败，不能覆盖重建后渠道的记录。
			fresh := &ChannelUpstreamRatio{ChannelId: 2, Mode: UpstreamRatioModeManual, Ratio: "0.7", ChannelCreatedTime: 6000}
			require.NoError(t, SaveChannelUpstreamRatio(fresh, 0, nil))
			inflight := &ChannelUpstreamRatio{ChannelId: 2, Mode: UpstreamRatioModeAuto, Ratio: "9", ChannelCreatedTime: 5000}
			require.ErrorIs(t, SaveChannelUpstreamRatio(inflight, 1, nil), ErrUpstreamRatioConflict)
			kept, err := GetChannelUpstreamRatio(2)
			require.NoError(t, err)
			require.Equal(t, "0.7", kept.Ratio)
		})
	}
}
