package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type gatewayStartupLegacyToken struct {
	ID          int    `gorm:"primaryKey"`
	UserID      int    `gorm:"index"`
	Key         string `gorm:"type:varchar(128);uniqueIndex"`
	Name        string `gorm:"index"`
	RemainQuota int
	ModelLimits string `gorm:"type:varchar(1024)"`
}

type gatewayStartupLegacyUser struct {
	ID          int     `gorm:"primaryKey"`
	Username    string  `gorm:"unique;index"`
	Password    string  `gorm:"not null"`
	AccessToken *string `gorm:"type:char(32);uniqueIndex"`
	AffCode     string  `gorm:"type:varchar(32);uniqueIndex"`
	Role        int
	Status      int
	Group       string `gorm:"type:varchar(64)"`
}

type gatewayStartupLegacyRecovery struct {
	ID             int64  `gorm:"primaryKey"`
	RequestID      string `gorm:"type:varchar(128);uniqueIndex;not null"`
	UserID         int    `gorm:"not null"`
	TokenID        int    `gorm:"not null"`
	Source         string `gorm:"type:varchar(32);not null"`
	FundingAmount  int64  `gorm:"not null;default:0"`
	SubscriptionID int
	ExtraAmount    int64  `gorm:"not null;default:0"`
	ExtraRefund    string `gorm:"type:varchar(16);not null;default:'pending'"`
	Status         string `gorm:"type:varchar(32);not null"`
}

type gatewayStartupLegacyState struct {
	ID           uint64 `gorm:"primaryKey"`
	UserID       int
	TokenID      int
	Kind         string `gorm:"type:varchar(32)"`
	Digest       string `gorm:"type:varchar(64)"`
	ChannelID    int
	CredentialID string `gorm:"type:varchar(36)"`
	EndpointID   string `gorm:"type:varchar(64)"`
	CreatedAt    int64
	ExpiresAt    int64
}

type gatewayStartupLegacyMidjourney struct {
	Id               int `gorm:"primaryKey"`
	UserId           int
	TokenId          int
	BillingChannelId int
	MjId             string
	Quota            int
	Status           string
}

func TestGatewayStartupMigrations(t *testing.T) {
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
				t.Skip(test.env + " must target an isolated database service with permission to create test databases")
			}
			open := gatewayStartupDatabase(t, test.kind, dsn)
			db, err := open()
			require.NoError(t, err)
			previousDB, previousLogDB := DB, LOG_DB
			previousMainKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(test.kind, test.kind)
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousMainKind, previousLogKind)
				initCol()
				sqlDB, closeErr := db.DB()
				if assert.NoError(t, closeErr) {
					assert.NoError(t, sqlDB.Close())
				}
			})
			require.NoError(t, db.Table("tokens").AutoMigrate(&gatewayStartupLegacyToken{}))
			require.NoError(t, db.Table("users").AutoMigrate(&gatewayStartupLegacyUser{}))
			require.NoError(t, db.Table("billing_recoveries").AutoMigrate(&gatewayStartupLegacyRecovery{}))
			require.NoError(t, db.Table("gateway_state_bindings").AutoMigrate(&gatewayStartupLegacyState{}))
			require.NoError(t, db.Table("midjourneys").AutoMigrate(&gatewayStartupLegacyMidjourney{}))
			require.NoError(t, db.Table("midjourneys").Create(&gatewayStartupLegacyMidjourney{Id: 41, UserId: 17, TokenId: 23, BillingChannelId: 37, MjId: "legacy-mj", Quota: 29, Status: "SUBMITTED"}).Error)
			require.NoError(t, db.Table("gateway_state_bindings").Create(&gatewayStartupLegacyState{UserID: 17, TokenID: 23, Kind: "response", Digest: GatewayDigest("pre-transport-state"), ChannelID: 37, CredentialID: "legacy-credential", EndpointID: "legacy-endpoint", ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
			require.NoError(t, db.Table("users").Create(&gatewayStartupLegacyUser{ID: 17, Username: "startup-admin", Password: "fixture-password-hash", Role: common.RoleRootUser, Status: common.UserStatusEnabled, Group: "team"}).Error)
			require.NoError(t, db.Table("tokens").Create(&gatewayStartupLegacyToken{ID: 23, UserID: 17, Key: "local-startup-fixture", Name: "legacy-token", RemainQuota: 71, ModelLimits: "model-before-upgrade"}).Error)
			require.NoError(t, db.Table("billing_recoveries").Create(&gatewayStartupLegacyRecovery{ID: 31, RequestID: "startup-recovery", UserID: 17, TokenID: 23, Source: "wallet", FundingAmount: 73, Status: BillingRecoveryStatusActive}).Error)
			retiredRecovery := gatewayStartupLegacyRecovery{ID: 32, RequestID: "retired-recovery", UserID: 17, TokenID: 23, Source: "subscription", FundingAmount: 41, SubscriptionID: 61, ExtraAmount: 7, ExtraRefund: "pending", Status: BillingRecoveryStatusRefundPending}
			require.NoError(t, db.Table("billing_recoveries").Create(&retiredRecovery).Error)
			// Retired product data remains available for offline reconciliation.
			// The application must not migrate or delete these historical tables.
			type archivedRow struct {
				ID       int `gorm:"primaryKey"`
				Evidence string
			}
			archivedTables := []string{"subscription_plans", "subscription_orders", "user_subscriptions", "subscription_pre_consume_records"}
			for _, table := range archivedTables {
				require.NoError(t, db.Table(table).AutoMigrate(&archivedRow{}))
				require.NoError(t, db.Table(table).Create(&archivedRow{ID: 1, Evidence: "preserve-for-reconciliation"}).Error)
			}
			assert.False(t, db.Migrator().HasColumn(&Token{}, "conversion_policy"))
			assert.False(t, db.Migrator().HasColumn(&User{}, "gateway_concurrency"))
			assert.False(t, db.Migrator().HasColumn(&BillingRecovery{}, "pre_consume_state"))

			err = migrateDB()
			require.NoError(t, err, "first complete startup migration: %s", db.Logger.(*gatewayStartupMigrationRecorder).failedDDL)
			var token Token
			require.NoError(t, db.First(&token, 23).Error)
			assert.Equal(t, "safe", token.ConversionPolicy)
			assert.Equal(t, 71, token.RemainQuota)
			assert.Equal(t, "model-before-upgrade", token.ModelLimits)
			config, err := GetGatewayConfig()
			require.NoError(t, err)
			assert.Equal(t, DefaultGatewayConfig(), config)
			var recovery BillingRecovery
			require.NoError(t, db.First(&recovery, 31).Error)
			assert.Equal(t, int64(73), recovery.FundingAmount)
			assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
			assert.Equal(t, BillingRecoveryPreConsumePrepared, recovery.PreConsumeState)
			legacyState, err := FindGatewayState(context.Background(), 17, 23, "response", "pre-transport-state")
			require.NoError(t, err)
			assert.Equal(t, "legacy-credential", legacyState.CredentialID)
			assert.Empty(t, legacyState.TransportID, "migration must not invent an actual transport identity")
			var legacyMJ Midjourney
			require.NoError(t, db.First(&legacyMJ, 41).Error)
			assert.Equal(t, 29, legacyMJ.Quota)
			assert.Equal(t, 23, legacyMJ.TokenId)
			assert.Equal(t, 37, legacyMJ.BillingChannelId)
			assert.Empty(t, legacyMJ.PrivateData.CredentialID, "migration must not guess the historical provider account")
			legacyMJ.PrivateData = TaskPrivateData{CredentialVersion: 1, CredentialID: "stable-mj-credential", EndpointID: "stable-mj-endpoint", TokenId: 23, SubmissionRequestID: "mj-submission"}
			require.NoError(t, db.Model(&legacyMJ).Update("private_data", legacyMJ.PrivateData).Error)

			zero, seven := 0, 7
			config.UserConcurrency, config.WaitMS, config.UserQueueSize = 19, 4000, 8
			config.Groups["team"] = GatewayAdmissionOverride{Concurrency: &seven, WaitMS: &zero}
			require.NoError(t, SaveGatewayConfig(config))
			config.Version++
			require.NoError(t, db.Model(&Token{}).Where("id = ?", 23).Update("conversion_policy", "strict").Error)
			for id, policy := range map[int]any{24: "", 25: nil} {
				require.NoError(t, db.Model(&Token{}).Create(map[string]any{"id": id, "user_id": 17, "key": fmt.Sprintf("startup-uninitialized-%d", id), "conversion_policy": policy}).Error)
			}
			require.NoError(t, db.Model(&User{}).Where("id = ?", 17).Updates(map[string]any{"gateway_concurrency": 0, "gateway_wait_ms": 0, "gateway_queue_size": 0}).Error)
			var marker Option
			require.NoError(t, db.Where(map[string]any{"key": "gateway_runtime_v2_migrated_at"}).First(&marker).Error)

			// A fresh connection exercises restart behavior instead of reusing
			// the migrator's connection and prepared-statement state.
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
			db, err = open()
			require.NoError(t, err)
			DB, LOG_DB = db, db
			initCol()
			err = migrateDB()
			require.NoError(t, err, "second complete startup migration: %s", db.Logger.(*gatewayStartupMigrationRecorder).failedDDL)
			var archivedRecovery gatewayStartupLegacyRecovery
			require.NoError(t, db.Table("billing_recoveries").First(&archivedRecovery, retiredRecovery.ID).Error)
			assert.Equal(t, retiredRecovery, archivedRecovery, "retired source and amount evidence must survive upgrades")
			for _, table := range archivedTables {
				var row archivedRow
				require.NoError(t, db.Table(table).First(&row, 1).Error)
				assert.Equal(t, "preserve-for-reconciliation", row.Evidence)
			}
			stored, err := GetGatewayConfig()
			require.NoError(t, err)
			assert.Equal(t, config, stored, "startup must preserve administrator configuration and version")
			require.NoError(t, db.First(&token, 23).Error)
			assert.Equal(t, "strict", token.ConversionPolicy)
			for _, id := range []int{24, 25} {
				var upgraded Token
				require.NoError(t, db.First(&upgraded, id).Error)
				assert.Equal(t, "safe", upgraded.ConversionPolicy, "empty and NULL legacy policies must be backfilled")
			}
			var user User
			require.NoError(t, db.First(&user, 17).Error)
			assert.Equal(t, &zero, user.GatewayConcurrency)
			assert.Equal(t, &zero, user.GatewayWaitMS)
			assert.Equal(t, &zero, user.GatewayQueueSize)
			var afterMarker Option
			require.NoError(t, db.Where(map[string]any{"key": marker.Key}).First(&afterMarker).Error)
			assert.Equal(t, marker.Value, afterMarker.Value)
			var restartedMJ Midjourney
			require.NoError(t, db.First(&restartedMJ, 41).Error)
			assert.Equal(t, legacyMJ.PrivateData, restartedMJ.PrivateData)
			assert.Equal(t, 29, restartedMJ.Quota)
			gatewayStartupSchema(t, db)
			gatewayStartupUniqueConstraints(t, db)
			var version string
			if test.kind == common.DatabaseTypeSQLite {
				require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
			} else {
				require.NoError(t, db.Raw("SELECT VERSION()").Scan(&version).Error)
			}
			if !t.Failed() {
				t.Logf("two complete migrations passed on %s %s; legacy values and administrator overrides preserved", test.kind, version)
			}
		})
	}
}

func gatewayStartupSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, target := range []struct {
		model   any
		columns []string
		indexes []string
	}{
		{&Token{}, []string{"conversion_policy"}, nil},
		{&User{}, []string{"gateway_concurrency", "gateway_wait_ms", "gateway_queue_size"}, nil},
		{&Channel{}, []string{"gateway_identity_version"}, nil},
		{&ChannelHealth{}, []string{"probe_owner", "probe_expires_at", "health_epoch"}, []string{"channel_health_scope"}},
		{&GatewayCredential{}, []string{"id", "channel_id", "fingerprint"}, []string{"gateway_credential_lookup"}},
		{&GatewayStateBinding{}, []string{"user_id", "token_id", "credential_id", "endpoint_id", "transport_id", "expires_at"}, []string{"gateway_state_lookup", "idx_gateway_state_bindings_expires_at"}},
		{&BillingRecovery{}, []string{"pre_consume_state", "token_required", "funding_settlement", "token_settlement", "consumption_snapshot", "consumption_state", "consumption_statistics", "consumption_log", "consumption_error"}, []string{"billing_recovery_pending_scan", "idx_billing_recoveries_request_id"}},
		{&BillingRecoveryAdjustment{}, []string{"request_id", "component", "operation", "amount"}, []string{"billing_recovery_adjustment_key"}},
	} {
		require.True(t, db.Migrator().HasTable(target.model), "%T table", target.model)
		for _, column := range target.columns {
			assert.True(t, db.Migrator().HasColumn(target.model, column), "%T.%s", target.model, column)
		}
		for _, index := range target.indexes {
			assert.True(t, db.Migrator().HasIndex(target.model, index), "%T index %s", target.model, index)
		}
	}
	// Verify NULL semantics by writing and reading them: SQLite's driver may
	// report a nullable column as non-nullable through ColumnTypes metadata.
	require.NoError(t, db.Model(&User{}).Where("id = ?", 17).Updates(map[string]any{"gateway_concurrency": nil, "gateway_wait_ms": nil, "gateway_queue_size": nil}).Error)
	var inherited User
	require.NoError(t, db.First(&inherited, 17).Error)
	assert.Nil(t, inherited.GatewayConcurrency)
	assert.Nil(t, inherited.GatewayWaitMS)
	assert.Nil(t, inherited.GatewayQueueSize)
}

func gatewayStartupUniqueConstraints(t *testing.T, db *gorm.DB) {
	t.Helper()
	credential := GatewayCredential{ID: uuid.NewString(), ChannelID: 37, Fingerprint: GatewayDigest("local-startup-credential")}
	require.NoError(t, db.Create(&credential).Error)
	duplicate := credential
	duplicate.ID = uuid.NewString()
	assert.Error(t, db.Create(&duplicate).Error, "credential lookup must remain unique")
	binding := GatewayStateBinding{UserID: 17, TokenID: 23, Kind: "response", Digest: GatewayDigest("local-state"), ChannelID: 37, CredentialID: credential.ID, EndpointID: "local-endpoint", ExpiresAt: 1}
	require.NoError(t, db.Create(&binding).Error)
	binding.ID = 0
	assert.Error(t, db.Create(&binding).Error, "state ownership lookup must remain unique")
	recovery := BillingRecovery{RequestID: "startup-recovery", UserID: 17, TokenID: 23, Source: "wallet"}
	assert.Error(t, db.Create(&recovery).Error, "logical request accounting must remain unique")
	adjustment := BillingRecoveryAdjustment{RequestID: "startup-recovery", Component: BillingRecoveryComponentToken, Operation: BillingRecoveryOperationSettle, Amount: 2}
	require.NoError(t, db.Create(&adjustment).Error)
	adjustment.ID = 0
	assert.Error(t, db.Create(&adjustment).Error, "an accounting effect cannot be duplicated")
}

func gatewayStartupDatabase(t *testing.T, kind common.DatabaseType, dsn string) func() (*gorm.DB, error) {
	t.Helper()
	config := &gorm.Config{Logger: &gatewayStartupMigrationRecorder{Interface: logger.Default.LogMode(logger.Silent)}}
	if kind == common.DatabaseTypeSQLite {
		path := filepath.Join(t.TempDir(), "gateway-startup.db")
		return func() (*gorm.DB, error) { return gorm.Open(sqlite.Open(path), config) }
	}
	name := "gateway_startup_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	var admin *gorm.DB
	var open func() (*gorm.DB, error)
	var quoted string
	var err error
	if kind == common.DatabaseTypeMySQL {
		parsed, parseErr := mysqldriver.ParseDSN(dsn)
		require.NoError(t, parseErr)
		admin, err = gorm.Open(mysql.Open(dsn), config)
		require.NoError(t, err)
		quoted = "`" + name + "`"
		parsed.DBName = name
		open = func() (*gorm.DB, error) { return gorm.Open(mysql.Open(parsed.FormatDSN()), config) }
	} else {
		parsed, parseErr := pgx.ParseConfig(dsn)
		require.NoError(t, parseErr)
		admin, err = gorm.Open(postgres.New(postgres.Config{DSN: dsn, PreferSimpleProtocol: true}), config)
		require.NoError(t, err)
		quoted = pgx.Identifier{name}.Sanitize()
		parsed.Database = name
		parsed.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
		open = func() (*gorm.DB, error) {
			connection := stdlib.OpenDB(*parsed)
			db, err := gorm.Open(postgres.New(postgres.Config{Conn: connection}), config)
			if err != nil {
				_ = connection.Close()
			}
			return db, err
		}
	}
	adminSQL, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, adminSQL.Close()) })
	statement := "CREATE DATABASE " + quoted
	if kind == common.DatabaseTypeMySQL {
		statement += " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	}
	require.NoError(t, admin.Exec(statement).Error)
	// Register DROP only after this test successfully creates its unique name.
	t.Cleanup(func() {
		assert.NoError(t, admin.Exec("DROP DATABASE "+quoted).Error, fmt.Sprintf("remove test-created database %s", name))
	})
	return open
}

type gatewayStartupMigrationRecorder struct {
	logger.Interface
	failedDDL string
}

func (recorder *gatewayStartupMigrationRecorder) Trace(_ context.Context, _ time.Time, query func() (string, int64), err error) {
	if err == nil {
		return
	}
	statement, _ := query()
	upper := strings.ToUpper(strings.TrimSpace(statement))
	if strings.HasPrefix(upper, "ALTER TABLE") || strings.HasPrefix(upper, "CREATE TABLE") || strings.HasPrefix(upper, "CREATE INDEX") {
		recorder.failedDDL = statement
	}
}
