package model

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

var hardDeleteTables = []any{&User{}, &UserAccessToken{}, &ExternalIdentityClaim{}, &TwoFABackupCode{}, &TwoFA{}, &UserSession{}, &AuthFlow{}, &PasskeyCredential{}, &UserOAuthBinding{}}

func TestTokenRoutingDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("ROUTING_TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("ROUTING_TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("ROUTING_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("ROUTING_TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			prefix := fmt.Sprintf("route_%d_", time.Now().UnixNano())
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				sqlDB.SetMaxOpenConns(1)
			}
			require.NoError(t, db.AutoMigrate(&Token{}, &TokenRouteProfile{}, &TokenRouteEntry{}, &Channel{}, &Ability{}))
			// 硬删除用户的屏障用例会经过 HardDelete 触及的全部表。
			require.NoError(t, db.AutoMigrate(hardDeleteTables...))
			barrier := installRouteBarrier(t, db)
			previous, redis, cache, databaseType := DB, common.RedisEnabled, common.MemoryCacheEnabled, common.MainDatabaseType()
			DB = db
			common.RedisEnabled = false
			common.MemoryCacheEnabled = false
			// lockForUpdate 按主库类型决定是否加锁，不设置时 MySQL/PostgreSQL 也会按 SQLite 跳过。
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			t.Cleanup(func() {
				DB = previous
				common.RedisEnabled = redis
				common.MemoryCacheEnabled = cache
				common.SetMainDatabaseType(databaseType)
				require.NoError(t, db.Migrator().DropTable(&TokenRouteEntry{}, &TokenRouteProfile{}, &Token{}, &Ability{}, &Channel{}))
				require.NoError(t, db.Migrator().DropTable(hardDeleteTables...))
				require.NoError(t, sqlDB.Close())
			})
			token := Token{UserId: 17, Key: "routing-db-fixture"}
			require.NoError(t, token.Insert())
			require.NoError(t, DB.Create(&TokenRouteProfile{TokenID: token.Id, Mode: TokenRouteOrdered, SelectedOnly: true, Version: 1}).Error)
			for _, id := range []int{11, 22} {
				require.NoError(t, db.Create(&Channel{Id: id, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "route-model"}).Error)
				require.NoError(t, db.Create(&Ability{ChannelId: id, Model: "route-model", Enabled: true}).Error)
			}
			rows, selectionErr := RouteChannelRows(context.Background(), "route-model", []dto.ChannelFilter{{Kind: dto.FilterTokenRoute, ChannelIDs: []int{22, 11}}})
			require.NoError(t, selectionErr)
			require.Len(t, rows, 2)
			ctx := context.Background()
			input := TokenRouting{TokenRouteProfile: TokenRouteProfile{SelectedOnly: true, Mode: TokenRouteOrdered, Version: 1}, Entries: []TokenRouteEntry{{ChannelID: 11, Enabled: true}, {ChannelID: 22, Enabled: false}}}
			var wg sync.WaitGroup
			outcomes := make(chan error, 8)
			for range 8 {
				wg.Go(func() {
					_, err := SaveTokenRouting(ctx, 17, token.Id, input, map[int]bool{11: true, 22: true})
					outcomes <- err
				})
			}
			wg.Wait()
			close(outcomes)
			success := 0
			for err := range outcomes {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, ErrTokenRouteConflict)
				}
			}
			require.Equal(t, 1, success, "one editor commits the requested revision")
			view, err := ReadTokenRouting(ctx, token.Id)
			require.NoError(t, err)
			require.Equal(t, int64(2), view.Version)
			require.Len(t, view.Entries, 2)
			require.Equal(t, 11, view.Entries[0].ChannelID)
			_, err = SaveTokenRouting(ctx, 99, token.Id, view, nil)
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
			view, err = SaveTokenRouting(ctx, 17, token.Id, TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteAuto, Version: 2}}, nil)
			require.NoError(t, err)
			require.Equal(t, int64(3), view.Version)
			require.Empty(t, view.Entries)
			require.NoError(t, token.Delete())
			var count int64
			require.NoError(t, db.Model(&TokenRouteProfile{}).Count(&count).Error)
			require.Zero(t, count)

			// 删除渠道后的孤儿条目，以及硬删除用户时连同软删除令牌的路由，都要清干净。
			owner := Token{UserId: 18, Key: "routing-db-owner"}
			require.NoError(t, owner.Insert())
			require.NoError(t, DB.Create(&TokenRouteProfile{TokenID: owner.Id, Mode: TokenRouteOrdered, SelectedOnly: true, Version: 1}).Error)
			retired := Token{UserId: 18, Key: "routing-db-retired"}
			require.NoError(t, retired.Insert())
			require.NoError(t, DB.Create(&TokenRouteProfile{TokenID: retired.Id, Mode: TokenRouteOrdered, SelectedOnly: true, Version: 1}).Error)
			require.NoError(t, db.Delete(&retired).Error)
			require.NoError(t, db.Create(&[]TokenRouteEntry{{TokenID: owner.Id, ChannelID: 11, Enabled: true}, {TokenID: owner.Id, ChannelID: 22, Position: 1, Enabled: true}, {TokenID: retired.Id, ChannelID: 11, Enabled: true}}).Error)
			require.NoError(t, db.Delete(&Channel{}, 22).Error)
			require.NoError(t, DeleteOrphanChannelRouteEntries(ctx))
			var remaining []TokenRouteEntry
			require.NoError(t, db.Order("token_id, channel_id").Find(&remaining).Error)
			require.Equal(t, []TokenRouteEntry{{TokenID: owner.Id, ChannelID: 11, Enabled: true}, {TokenID: retired.Id, ChannelID: 11, Enabled: true}}, remaining)
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return deleteUserTokenRouting(tx, 18) }))
			require.NoError(t, db.Model(&TokenRouteEntry{}).Count(&count).Error)
			require.Zero(t, count)
			require.NoError(t, db.Model(&TokenRouteProfile{}).Count(&count).Error)
			require.Zero(t, count)

			runTokenRouteRaceCases(t, db, barrier)
		})
	}
}
