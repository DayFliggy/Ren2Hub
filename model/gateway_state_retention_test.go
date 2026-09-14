package model

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayStateRetentionDatabases(t *testing.T) {
	for _, test := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"},
	} {
		t.Run(string(test.kind), func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " must target an isolated database service")
			}
			db, err := gatewayStartupDatabase(t, test.kind, dsn)()
			require.NoError(t, err)
			previous := DB
			DB = db
			t.Cleanup(func() { DB = previous; connection, _ := db.DB(); _ = connection.Close() })
			require.NoError(t, db.AutoMigrate(&GatewayStateBinding{}))
			ctx := context.Background()
			now := time.Now().Unix()
			binding := GatewayStateBinding{UserID: 1, TokenID: 2, Kind: "response", ChannelID: 3, CredentialID: "credential", EndpointID: "endpoint", TransportID: "transport"}
			require.NoError(t, BindGatewayState(ctx, binding, "shorter"))
			binding.ExpiresAt = now + 3600
			require.NoError(t, BindGatewayState(ctx, binding, "shorter"))
			binding.ExpiresAt = now + 7200
			require.NoError(t, BindGatewayState(ctx, binding, "shorter"))
			stored, err := FindGatewayState(ctx, 1, 2, "response", "shorter")
			require.NoError(t, err)
			assert.Equal(t, now+3600, stored.ExpiresAt)
			assert.Equal(t, binding.TransportID, stored.TransportID)
			binding.TransportID = "another-transport"
			require.ErrorIs(t, BindGatewayState(ctx, binding, "shorter"), ErrGatewayStateUnknown)
			binding.TransportID = "transport"
			binding.ChannelID = 4
			require.ErrorIs(t, BindGatewayState(ctx, binding, "shorter"), ErrGatewayStateUnknown)
			binding.ChannelID = 3
			binding.ExpiresAt = now - 1
			require.ErrorIs(t, BindGatewayState(ctx, binding, "already-expired"), ErrGatewayStateUnknown)
			binding.ExpiresAt = now + 90*24*3600
			require.NoError(t, BindGatewayState(ctx, binding, "capped"))
			stored, err = FindGatewayState(ctx, 1, 2, "response", "capped")
			require.NoError(t, err)
			assert.InDelta(t, 30*24*3600, stored.ExpiresAt-now, 2)

			for _, value := range []string{"expired-one", "expired-two"} {
				record := binding
				record.Digest, record.ExpiresAt = GatewayDigest(value), now-1
				require.NoError(t, db.Create(&record).Error)
			}
			for index, expected := range []int64{1, 1, 0} {
				deleted, err := DeleteExpiredGatewayStates(ctx, now, 1)
				require.NoError(t, err)
				assert.Equal(t, expected, deleted, "cleanup batch %d", index)
			}
			var remaining int64
			require.NoError(t, db.Model(&GatewayStateBinding{}).Count(&remaining).Error)
			assert.EqualValues(t, 2, remaining, "cleanup retains both unexpired bindings")
			_, err = FindGatewayState(ctx, 1, 2, "response", "shorter")
			require.NoError(t, err)
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			_, err = DeleteExpiredGatewayStates(cancelled, now, 1000)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}
