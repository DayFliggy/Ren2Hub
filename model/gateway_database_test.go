package model

import (
	"context"
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
)

func TestGatewayDatabaseContracts(t *testing.T) {
	for _, test := range []struct {
		name, env string
		kind      common.DatabaseType
		open      func(string) gorm.Dialector
	}{
		{"sqlite", "", common.DatabaseTypeSQLite, func(_ string) gorm.Dialector { return sqlite.Open("file:gateway-contracts?mode=memory&cache=shared") }},
		{"mysql", "TEST_MYSQL_DSN", common.DatabaseTypeMySQL, func(s string) gorm.Dialector { return mysql.Open(s) }},
		{"postgres", "TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL, func(s string) gorm.Dialector { return postgres.Open(s) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " not configured")
			}
			db, err := gorm.Open(test.open(dsn), &gorm.Config{})
			require.NoError(t, err)
			previous, previousKind := DB, common.MainDatabaseType()
			DB = db
			common.SetMainDatabaseType(test.kind)
			t.Cleanup(func() {
				DB = previous
				common.SetMainDatabaseType(previousKind)
				sqlDB, _ := db.DB()
				_ = sqlDB.Close()
			})
			for pass := 0; pass < 2; pass++ {
				require.NoError(t, db.AutoMigrate(&Option{}, &Token{}, &User{}, &Channel{}, &GatewayCredential{}, &GatewayStateBinding{}))
				require.NoError(t, migrateRoutingModels(db))
				require.NoError(t, migrateGatewayDefaults(db))
			}
			config, err := GetGatewayConfig()
			require.NoError(t, err)
			original := config.UserConcurrency
			config.UserConcurrency = 19
			require.NoError(t, SaveGatewayConfig(config))
			require.ErrorIs(t, SaveGatewayConfig(config), ErrGatewayConfigConflict)
			require.NoError(t, migrateGatewayDefaults(db))
			current, err := GetGatewayConfig()
			require.NoError(t, err)
			assert.Equal(t, 19, current.UserConcurrency)
			current.UserConcurrency = original
			require.NoError(t, SaveGatewayConfig(current))
			identity, err := EnsureGatewayCredential(context.Background(), 991003, "isolated-test-credential")
			require.NoError(t, err)
			second, err := EnsureGatewayCredential(context.Background(), 991003, "isolated-test-credential")
			require.NoError(t, err)
			assert.Equal(t, identity.ID, second.ID)
			binding := GatewayStateBinding{UserID: 991003, TokenID: 991003, Kind: "response", ChannelID: 991003, CredentialID: identity.ID, EndpointID: "test-endpoint", ExpiresAt: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, BindGatewayState(context.Background(), binding, "isolated-response"))
			require.NoError(t, BindGatewayState(context.Background(), binding, "isolated-response"))
			_, err = FindGatewayState(context.Background(), 991003, 991004, "response", "isolated-response")
			require.ErrorIs(t, err, ErrGatewayStateUnknown)
		})
	}
}
