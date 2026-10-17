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
			previous, redis, cache := DB, common.RedisEnabled, common.MemoryCacheEnabled
			DB = db
			common.RedisEnabled = false
			common.MemoryCacheEnabled = false
			t.Cleanup(func() {
				DB = previous
				common.RedisEnabled = redis
				common.MemoryCacheEnabled = cache
				require.NoError(t, db.Migrator().DropTable(&TokenRouteEntry{}, &TokenRouteProfile{}, &Token{}, &Ability{}, &Channel{}))
				require.NoError(t, sqlDB.Close())
			})
			token := Token{UserId: 17, Key: "routing-db-fixture"}
			require.NoError(t, token.InsertWithRouting(TokenRouteManual))
			for _, id := range []int{11, 22} {
				require.NoError(t, db.Create(&Channel{Id: id, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Group: "default", Models: "route-model"}).Error)
				require.NoError(t, db.Create(&Ability{ChannelId: id, Group: "default", Model: "route-model", Enabled: true}).Error)
			}
			selected, selectionErr := GetRandomSatisfiedChannel("default", "route-model", 0, []dto.ChannelFilter{{Kind: dto.FilterTokenRoute, ChannelIDs: []int{22, 11}}})
			require.NoError(t, selectionErr)
			require.NotNil(t, selected)
			require.Equal(t, 22, selected.Id)
			ctx := context.Background()
			input := TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: 1}, Entries: []TokenRouteEntry{{ChannelID: 11, Enabled: true}, {ChannelID: 22, Enabled: false}}}
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
		})
	}
}
