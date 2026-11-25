package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

// E2-3：启用状态和 type 57 退役都由 ChannelSatisfiesFilters 统一判定，选路行查询不再自带一份。
func TestChannelSatisfiesFiltersOwnsRoutability(t *testing.T) {
	truncateTables(t)
	rows := []*Channel{
		{Id: 920001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled},
		{Id: 920002, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled},
		{Id: 920003, Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled},
		{Id: 920004, Type: constant.ChannelTypeAnthropic, Status: common.ChannelStatusEnabled},
	}
	for _, row := range rows {
		row.Name, row.Key, row.Models = "routable", "test-key", "routable-model"
		require.NoError(t, DB.Create(row).Error)
		require.NoError(t, DB.Create(&Ability{Model: "routable-model", ChannelId: row.Id, Enabled: true}).Error)
	}
	for _, row := range rows[1:3] {
		ok, kind := ChannelSatisfiesFilters(row, "routable-model", nil)
		require.False(t, ok, row.Id)
		require.Equal(t, dto.ChannelFilterKind("channel_unavailable"), kind)
	}
	found, err := RouteChannelRows(context.Background(), "routable-model", nil)
	require.NoError(t, err)
	require.Equal(t, []int{920001, 920004}, channelIDs(found))
}

// 与上游 GetChannel 和内存缓存相同：声明精确模型名的渠道优先，只有精确候选全部被过滤掉时
// 才回退到去掉 @ 修饰后的路由名。
func TestRouteChannelRowsPrefersExactModelName(t *testing.T) {
	truncateTables(t)
	const exact, base = "qwen3-max@thinking:on@temperature:0.2", "qwen3-max"
	for id, name := range map[int]string{930001: exact, 930002: base, 930003: base} {
		require.NoError(t, DB.Create(&Channel{Id: id, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "exact-first", Key: "test-key", Models: name}).Error)
		require.NoError(t, DB.Create(&Ability{Model: name, ChannelId: id, Enabled: true}).Error)
	}
	found, err := RouteChannelRows(context.Background(), exact, nil)
	require.NoError(t, err)
	require.Equal(t, []int{930001}, channelIDs(found), "an exact channel hides channels that only serve the routing name")
	found, err = RouteChannelRows(context.Background(), exact, []dto.ChannelFilter{{Kind: dto.FilterTokenRoute, ChannelIDs: []int{930002, 930003}}})
	require.NoError(t, err)
	require.Equal(t, []int{930002, 930003}, channelIDs(found), "the routing name is used once filters remove every exact channel")
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", 930001).Update("status", common.ChannelStatusManuallyDisabled).Error)
	found, err = RouteChannelRows(context.Background(), exact, nil)
	require.NoError(t, err)
	require.Equal(t, []int{930002, 930003}, channelIDs(found), "a disabled exact channel does not block the fallback")
}

// TestReadRouteChannelsAndModelsDatabaseMatrix verifies across SQLite (using the
// production WAL + _txlock=immediate DSN), MySQL, and PostgreSQL that:
//  1. A concurrent write transaction commits without being blocked by the catalog
//     read transaction (proving SQLite uses a deferred read-only snapshot rather
//     than acquiring BEGIN IMMEDIATE's global write lock).
//  2. ReadRouteChannelsAndModels reads both channels and enabled abilities from
//     the same pre-commit snapshot instead of mixing generation v1 channels with
//     generation v2 abilities.
func TestReadRouteChannelsAndModelsDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				dsn := filepath.Join(t.TempDir(), "route-snapshot.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate"
				driver = sqlite.Open(dsn)
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			prefix := fmt.Sprintf("route_snap_%d_", time.Now().UnixNano())
			db, err := gorm.Open(driver, &gorm.Config{
				NamingStrategy: schema.NamingStrategy{TablePrefix: prefix},
				Logger:         logger.Default.LogMode(logger.Silent),
			})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(4)

			previousDB, previousType := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			initCol()
			t.Cleanup(func() {
				afterReadRouteChannelsHook = nil
				DB = previousDB
				common.SetMainDatabaseType(previousType)
				initCol()
				assert.NoError(t, db.Migrator().DropTable(&Ability{}, &Channel{}))
				assert.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))

			require.NoError(t, db.Create(&Channel{
				Id: 940001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled,
				Name: "snap-v1", Key: "test-key", Models: "pre-model",
			}).Error)
			require.NoError(t, db.Create(&Ability{Model: "pre-model", ChannelId: 940001, Enabled: true}).Error)

			writerErrCh := make(chan error, 1)
			afterReadRouteChannelsHook = func() {
				go func() {
					writerErrCh <- db.Transaction(func(wtx *gorm.DB) error {
						if err := wtx.Model(&Channel{}).Where("id = ?", 940001).Updates(map[string]any{
							"name":   "snap-v2",
							"models": "post-model",
						}).Error; err != nil {
							return err
						}
						if err := wtx.Model(&Ability{}).Where("channel_id = ? AND model = ?", 940001, "pre-model").Update("enabled", false).Error; err != nil {
							return err
						}
						return wtx.Create(&Ability{Model: "post-model", ChannelId: 940001, Enabled: true}).Error
					})
				}()
				select {
				case werr := <-writerErrCh:
					require.NoError(t, werr, "concurrent writer must commit without being blocked by read-only catalog snapshot")
				case <-time.After(2 * time.Second):
					t.Fatal("concurrent writer timed out; catalog read transaction unexpectedly held a write lock")
				}
			}

			rows, advertised, err := ReadRouteChannelsAndModels(context.Background())
			afterReadRouteChannelsHook = nil
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.Equal(t, "snap-v1", rows[0].Name, "first query sees pre-commit channel row")
			require.Equal(t, []string{"pre-model"}, advertised[940001], "second query inside same snapshot must still see pre-commit abilities")

			// A subsequent snapshot observes both committed updates atomically.
			rowsAfter, advertisedAfter, err := ReadRouteChannelsAndModels(context.Background())
			require.NoError(t, err)
			require.Len(t, rowsAfter, 1)
			require.Equal(t, "snap-v2", rowsAfter[0].Name)
			require.Equal(t, []string{"post-model"}, advertisedAfter[940001])
		})
	}
}

func channelIDs(rows []*Channel) []int {
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	return ids
}
