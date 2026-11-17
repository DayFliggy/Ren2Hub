package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestValidateOptionValueChecksBackupPaymentLinks(t *testing.T) {
	key := operation_setting.BackupPaymentLinksOptionKey
	require.Error(t, validateOptionValue(key, `[{"name":"Shop","url":"javascript:alert(1)"}]`))
	require.NoError(t, validateOptionValue(key, `[{"name":"Shop","url":"https://shop.example.com"}]`))
}

// The links are a JSON array kept in the options table; after a restart only
// the stored row can bring them back, and a rejected value must never be stored.
func TestBackupPaymentLinksOptionDatabaseRoundTrip(t *testing.T) {
	key := operation_setting.BackupPaymentLinksOptionKey
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			useBackupPaymentOptionDB(t, dialect)
			saved := []operation_setting.BackupPaymentLink{{Name: "兑换码商店", URL: "https://shop.example.com/redeem?ref=wallet&lang=zh", Icon: "telegram"}}
			value, err := common.Marshal(saved)
			require.NoError(t, err)
			require.NoError(t, UpdateOption(key, string(value)))
			require.Error(t, UpdateOption(key, `[{"name":"Shop","url":"javascript:alert(1)"}]`))

			require.NoError(t, updateOptionMap(key, "[]"))
			require.Empty(t, operation_setting.WalletBackupPaymentLinks(true, ""))
			require.NoError(t, LoadOptionsFromDatabase())
			require.Equal(t, saved, operation_setting.WalletBackupPaymentLinks(true, ""))
		})
	}
}

func useBackupPaymentOptionDB(t *testing.T, dialect string) {
	t.Helper()
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
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("bpay_%d_", time.Now().UnixNano())}, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	if dialect == "sqlite" {
		connection.SetMaxOpenConns(1)
	}
	require.NoError(t, db.AutoMigrate(&Option{}, &SystemTask{}))
	previousDB, previousType := DB, common.MainDatabaseType()
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	DB = db
	common.SetMainDatabaseType(dbType)
	t.Cleanup(func() {
		require.NoError(t, updateOptionMap(operation_setting.BackupPaymentLinksOptionKey, "[]"))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
		DB = previousDB
		common.SetMainDatabaseType(previousType)
		require.NoError(t, db.Migrator().DropTable(&Option{}, &SystemTask{}))
		require.NoError(t, connection.Close())
	})
}
