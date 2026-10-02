package model

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Non-SQLite DSNs must name this run's disposable database, never a live store.
func TestProviderUsageDatabaseContracts(t *testing.T) {
	for _, test := range []struct {
		name, env string
		kind      common.DatabaseType
		open      func(string) gorm.Dialector
	}{
		{"sqlite", "", common.DatabaseTypeSQLite, func(string) gorm.Dialector {
			return sqlite.Open("file:" + uuid.NewString() + "?mode=memory&cache=shared")
		}},
		{"mysql", "TEST_MYSQL_DSN", common.DatabaseTypeMySQL, func(dsn string) gorm.Dialector { return mysql.Open(dsn) }},
		{"postgres", "TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL, func(dsn string) gorm.Dialector { return postgres.Open(dsn) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			db, err := gorm.Open(test.open(dsn), &gorm.Config{})
			require.NoError(t, err)
			previous, previousKind := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(test.kind)
			t.Cleanup(func() {
				DB = previous
				common.SetMainDatabaseType(previousKind)
				sqlDB, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, sqlDB.Close())
			})
			for range 2 {
				require.NoError(t, db.AutoMigrate(&Channel{}, &GatewayCredential{}, &ProviderUsageSnapshot{}, &ProviderUsageSetting{}))
			}
			channel := &Channel{Type: constant.ChannelTypeCodex, Key: "provider-usage-fixture-" + uuid.NewString(), BaseURL: common.GetPointer("https://usage.invalid"), Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(channel).Error)
			t.Cleanup(func() {
				require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&ProviderUsageSnapshot{}).Error)
				require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&ProviderUsageSetting{}).Error)
				require.NoError(t, db.Where("channel_id = ?", channel.Id).Delete(&GatewayCredential{}).Error)
				require.NoError(t, db.Where("id = ?", channel.Id).Delete(&Channel{}).Error)
			})
			ctx, now := context.Background(), time.Now().Unix()
			setting, err := ReadProviderUsageSetting(ctx, channel.Id)
			require.NoError(t, err)
			assert.False(t, setting.Enabled)
			require.NoError(t, SaveProviderUsageSetting(ctx, channel.Id, true))
			require.NoError(t, SaveProviderUsageSetting(ctx, channel.Id, false))
			setting, err = ReadProviderUsageSetting(ctx, channel.Id)
			require.NoError(t, err)
			assert.False(t, setting.Enabled, "false must overwrite a previously enabled flag")
			identity, err := EnsureGatewayCredential(ctx, channel.Id, channel.Key)
			require.NoError(t, err)
			snapshot, err := BeginProviderUsageRefresh(ctx, channel.Id, identity.ID, channel.GatewayEndpointID(), now, 60)
			require.NoError(t, err)
			snapshot.State, snapshot.Windows, snapshot.FetchedAt, snapshot.ExpiresAt = "known", `[{"name":"rate_limit_primary","used_percent":0,"limit_seconds":null,"reset_at":null}]`, now, now+300
			require.NoError(t, FinishProviderUsageRefresh(ctx, snapshot))
			_, err = BeginProviderUsageRefresh(ctx, channel.Id, identity.ID, channel.GatewayEndpointID(), now+59, 60)
			require.ErrorIs(t, err, ErrProviderUsageCooldown)
			read, err := ReadProviderUsageSnapshot(ctx, channel)
			require.NoError(t, err)
			require.NotNil(t, read)
			assert.Equal(t, snapshot.Windows, read.Windows)
			oldKey := channel.Key
			require.NoError(t, RefreshGatewayCredential(ctx, channel, oldKey+"-refreshed"))
			channel.Key = oldKey + "-refreshed"
			refreshed, err := EnsureGatewayCredential(ctx, channel.Id, channel.Key)
			require.NoError(t, err)
			assert.Equal(t, identity.ID, refreshed.ID)
			read, err = ReadProviderUsageSnapshot(ctx, channel)
			require.NoError(t, err)
			require.NotNil(t, read, "trusted refresh retains cached observation ownership")
			require.Error(t, RefreshGatewayCredential(ctx, &Channel{Id: channel.Id, Key: oldKey}, "must-not-overwrite"))
			require.NoError(t, db.Model(&Channel{}).Where("id = ?", channel.Id).Update("key", "replacement-"+uuid.NewString()).Error)
			require.ErrorIs(t, FinishProviderUsageRefresh(ctx, snapshot), ErrProviderUsageIdentityChanged)
			var replaced Channel
			require.NoError(t, db.First(&replaced, channel.Id).Error)
			read, err = ReadProviderUsageSnapshot(ctx, &replaced)
			require.NoError(t, err)
			assert.Nil(t, read)
		})
	}
}
