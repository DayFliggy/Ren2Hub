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

func TestRequestDiagnosticsAcrossDatabases(t *testing.T) {
	for _, dialect := range []common.DatabaseType{common.DatabaseTypeSQLite, common.DatabaseTypeMySQL, common.DatabaseTypePostgreSQL} {
		t.Run(string(dialect), func(t *testing.T) {
			dsn := ""
			if dialect != common.DatabaseTypeSQLite {
				env := "TEST_MYSQL_DSN"
				if dialect == common.DatabaseTypePostgreSQL {
					env = "TEST_POSTGRES_DSN"
				}
				dsn = os.Getenv(env)
				if dsn == "" {
					t.Skip(env + " is not configured; compatibility unverified")
				}
			}
			db, err := gatewayStartupDatabase(t, dialect, dsn)()
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			previous := DB
			DB = db
			t.Cleanup(func() { DB = previous; assert.NoError(t, connection.Close()) })
			require.NoError(t, db.AutoMigrate(&GatewayRequestDiagnostic{}))
			require.NoError(t, db.AutoMigrate(&GatewayRequestDiagnostic{}))
			now := time.Now()
			for _, row := range []GatewayRequestDiagnostic{
				{RequestID: "owner-one", UserID: 1, TokenID: 11, TraceID: "same", ThreadID: "thread", CreatedAt: now.Unix(), AttemptsJSON: "[]"},
				{RequestID: "owner-two", UserID: 1, TokenID: 12, TraceID: "same", ThreadID: "thread", CreatedAt: now.Unix(), AttemptsJSON: "[]"},
				{RequestID: "other", UserID: 2, TokenID: 21, TraceID: "same", ThreadID: "thread", CreatedAt: now.Unix(), AttemptsJSON: "[]"},
				{RequestID: "expired", UserID: 1, TokenID: 11, CreatedAt: now.Add(-8 * 24 * time.Hour).Unix(), AttemptsJSON: "[]"},
			} {
				require.NoError(t, SaveGatewayRequestDiagnostic(context.Background(), &row))
			}
			items, total, err := QueryGatewayRequestDiagnostics(context.Background(), RequestDiagnosticFilter{UserID: 1, TokenID: 11, TraceID: "same"}, 0, 20, now.Add(-7*24*time.Hour).Unix())
			require.NoError(t, err)
			require.Len(t, items, 1)
			assert.EqualValues(t, 1, total)
			assert.Equal(t, "owner-one", items[0].RequestID)
			_, err = GetGatewayRequestDiagnostic(context.Background(), "other", 1, 0)
			require.Error(t, err)
			duplicate := GatewayRequestDiagnostic{RequestID: "owner-one", UserID: 1, TokenID: 99, CreatedAt: now.Unix(), AttemptsJSON: "[]"}
			require.NoError(t, SaveGatewayRequestDiagnostic(context.Background(), &duplicate))
			original, err := GetGatewayRequestDiagnostic(context.Background(), "owner-one", 1, 0)
			require.NoError(t, err)
			assert.Equal(t, 11, original.TokenID)
			removed, err := CleanupGatewayRequestDiagnostics(context.Background(), now.Add(-7*24*time.Hour))
			require.NoError(t, err)
			assert.EqualValues(t, 1, removed)
			_, err = GetGatewayRequestDiagnostic(context.Background(), "expired", 1, 0)
			require.Error(t, err)
			assert.NoError(t, db.Migrator().DropTable(&GatewayRequestDiagnostic{}))
		})
	}
}
