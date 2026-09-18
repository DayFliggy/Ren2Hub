package service

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestGatewayCitationStateDatabaseIntegration(t *testing.T) {
	for _, backend := range []struct {
		name, env string
		kind      common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite},
		{"mysql", "BILLING_TEST_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "BILLING_TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(backend.name, func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " is not configured")
			}
			mainDB, logDB := isolatedBillingDatabases(t, backend.kind, dsn)
			bindBillingIntegrationDatabases(t, mainDB, logDB, backend.kind)
			require.NoError(t, mainDB.AutoMigrate(&model.GatewayStateBinding{}, &model.GatewayCredential{}))
			assertGatewayStateOutputOwnership(t, mainDB)
		})
	}
}
