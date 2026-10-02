package model

import (
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyManagementTokenSchemaIsRejectedWithoutChanges(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "management-schema-test-key")
	for _, backend := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"},
	} {
		t.Run(string(backend.kind), func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " must target an isolated database")
			}
			db, err := gatewayStartupDatabase(t, backend.kind, dsn)()
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, connection.Close()) })
			require.NoError(t, db.AutoMigrate(&BillingSchema{}))
			cipher, err := billingEvidenceCipher()
			require.NoError(t, err)
			require.NoError(t, db.Create(&BillingSchema{ID: 1, Version: BillingSchemaVersion, EvidenceKeyID: cipher.KeyID()}).Error)
			require.NoError(t, db.Table("users").AutoMigrate(&gatewayStartupLegacyUser{}))
			legacy := "test-only-retired-token"
			require.NoError(t, db.Table("users").Create(&gatewayStartupLegacyUser{ID: 1, Username: "legacy", Password: "test", AffCode: "legacy", AccessToken: &legacy}).Error)
			var original gatewayStartupLegacyUser
			require.NoError(t, db.Table("users").First(&original, 1).Error)
			tablesBefore, err := db.Migrator().GetTables()
			require.NoError(t, err)
			require.ErrorContains(t, prepareBillingSchema(db), "legacy management token schema")
			require.ErrorContains(t, requireBillingSchema(db), "legacy management token schema")
			tablesAfter, err := db.Migrator().GetTables()
			require.NoError(t, err)
			assert.ElementsMatch(t, tablesBefore, tablesAfter)
			var retained gatewayStartupLegacyUser
			require.NoError(t, db.Table("users").First(&retained, 1).Error)
			require.NotNil(t, retained.AccessToken)
			assert.Equal(t, original, retained, "even database-specific CHAR padding is preserved")
		})
	}
}
