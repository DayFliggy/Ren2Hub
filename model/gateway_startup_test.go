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

type gatewayStartupLegacySystemInstance struct {
	NodeName string `gorm:"type:varchar(128);primaryKey"`
	Info     string `gorm:"type:text"`
}

func TestGatewayStartupMigrations(t *testing.T) {
	for _, test := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(test.kind), func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " is not configured")
			}
			open := gatewayStartupDatabase(t, test.kind, dsn)
			db, err := open()
			require.NoError(t, err)
			previousDB, previousLogs := DB, LOG_DB
			previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(test.kind, test.kind)
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogs
				common.SetDatabaseTypes(previousMain, previousLog)
				initCol()
				sqlDB, err := db.DB()
				if err == nil {
					_ = sqlDB.Close()
				}
			})
			require.NoError(t, migrateDB())
			require.False(t, db.Migrator().HasTable("system_instances"), "fresh installs do not create the retired heartbeat registry")
			require.True(t, db.Migrator().HasTable(&BillingSchema{}))
			require.True(t, db.Migrator().HasTable(&BillingEvent{}))
			require.False(t, db.Migrator().HasTable("billing_recovery_adjustments"))
			require.False(t, db.Migrator().HasTable("billing_upstream_costs"), "retired upstream cost records are not migrated")
			require.True(t, db.Migrator().HasColumn(&User{}, "reserved_quota"))
			require.True(t, db.Migrator().HasColumn(&Token{}, "available_quota"))
			user := User{Id: 17, Username: "startup", Quota: 5000000000}
			require.NoError(t, db.Create(&user).Error)
			gatewayStartupSchema(t, db)
			gatewayStartupUniqueConstraints(t, db)
			// A recognized database may still contain the previous monitor's
			// registry. The next startup retires it without touching user data.
			require.NoError(t, db.Table("system_instances").AutoMigrate(&gatewayStartupLegacySystemInstance{}))
			require.NoError(t, db.Table("system_instances").Create(&gatewayStartupLegacySystemInstance{NodeName: "retired-test-node", Info: "{}"}).Error)
			require.True(t, db.Migrator().HasTable("system_instances"))
			sqlDB, err := db.DB()
			require.NoError(t, err)
			require.NoError(t, sqlDB.Close())
			db, err = open()
			require.NoError(t, err)
			DB, LOG_DB = db, db
			initCol()
			require.NoError(t, migrateDB())
			require.False(t, db.Migrator().HasTable("system_instances"), "restart discards the retired heartbeat registry")
			require.NoError(t, dropRetiredSystemInstances(db), "retirement is idempotent")
			var restored User
			require.NoError(t, db.First(&restored, user.Id).Error)
			require.EqualValues(t, 5000000000, restored.Quota)
			t.Run("ops_queries", func(t *testing.T) { gatewayStartupOpsQueries(t, db) })
		})
	}
}

func gatewayStartupOpsQueries(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Create(&[]Channel{
		{Id: 501, Name: "ops-primary", Status: common.ChannelStatusEnabled, Key: "test-only-key-a"},
		{Id: 502, Name: "ops-backup", Status: common.ChannelStatusAutoDisabled, Key: "test-only-key-b"},
	}).Error)
	require.NoError(t, db.Create(&[]ChannelHealth{
		{ChannelID: 501, Model: "ops-model", KeyScope: "test-scope-a", State: RouteHealthStateOpen, FailureCount: 3, UpdatedAt: 200},
		{ChannelID: 501, Model: "ops-model", KeyScope: "test-scope-b", State: RouteHealthStateHalfOpen, FailureCount: 1, UpdatedAt: 190},
		{ChannelID: 502, Model: "ops-model", KeyScope: "test-scope-c", State: RouteHealthStateClosed, UpdatedAt: 300},
	}).Error)
	health, err := ReadOpsChannelHealth(1)
	require.NoError(t, err)
	assert.Equal(t, OpsChannelCounts{Total: 2, Enabled: 1, AutoDisabled: 1}, health.Channels)
	assert.Equal(t, OpsBreakerCounts{Closed: 1, Open: 1, HalfOpen: 1}, health.Breakers)
	require.Len(t, health.Unhealthy, 1)
	assert.Equal(t, "ops-primary", health.Unhealthy[0].ChannelName)
	assert.Equal(t, RouteHealthStateOpen, health.Unhealthy[0].State)
	assert.Equal(t, 3, health.Unhealthy[0].FailureCount)

	require.NoError(t, db.Create(&[]SystemTask{
		{TaskID: "ops-success", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusSucceeded, UpdatedAt: 100},
		{TaskID: "ops-failure", Type: SystemTaskTypeChannelTest, Status: SystemTaskStatusFailed, Error: "test task failed", UpdatedAt: 200},
		{TaskID: "ops-active", Type: SystemTaskTypeModelUpdate, Status: SystemTaskStatusRunning, UpdatedAt: 300},
	}).Error)
	activity, err := SummarizeSystemTaskActivity([]string{SystemTaskTypeChannelTest, SystemTaskTypeModelUpdate})
	require.NoError(t, err)
	assert.Equal(t, SystemTaskActivity{LastSuccessAt: 100, LastFailureAt: 200, LastError: "test task failed"}, activity[SystemTaskTypeChannelTest])
	assert.Equal(t, SystemTaskActivity{Active: true}, activity[SystemTaskTypeModelUpdate])

	require.NoError(t, db.Create(&[]PerfMetric{
		{ModelName: "ops-model", Group: "default", BucketTs: 3600, RequestCount: 3, SuccessCount: 2, TotalLatencyMs: 300},
		{ModelName: "ops-model", Group: "vip", BucketTs: 3600, RequestCount: 1, SuccessCount: 1, TotalLatencyMs: 200},
	}).Error)
	buckets, err := GetPerfMetricsSummaryBucketsAll(3600, 7200, nil)
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	assert.Equal(t, PerfMetricSummaryBucket{ModelName: "ops-model", BucketTs: 3600, RequestCount: 4, SuccessCount: 3, TotalLatencyMs: 500}, buckets[0])
	buckets, err = GetPerfMetricsSummaryBucketsAll(3600, 7200, []string{"default"})
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	assert.EqualValues(t, 3, buckets[0].RequestCount, "the reserved group column remains queryable")

	require.NoError(t, db.Create(&[]BillingRecovery{
		{RequestID: "ops-overdue", UserID: 17, Source: "wallet", Status: BillingRecoveryStatusActive, UpdatedAt: 100},
		{RequestID: "ops-recent", UserID: 17, Source: "wallet", Status: BillingRecoveryStatusActive, UpdatedAt: 900},
	}).Error)
	backlog, err := ReadBillingRecoveryBacklog(context.Background(), 500)
	require.NoError(t, err)
	assert.Equal(t, BillingRecoveryBacklog{Pending: 1, OldestUpdated: 100}, backlog, "ops only counts overdue unfinished recovery")
}

func TestGatewayStartupResumesEmptyMarkerButRejectsUnmarkedData(t *testing.T) {
	for _, test := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(test.kind), func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " is not configured; interrupted initialization is unverified")
			}
			db, err := gatewayStartupDatabase(t, test.kind, dsn)()
			require.NoError(t, err)
			connection, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, connection.Close()) })
			// Simulate a process exit between marker DDL and marker insertion.
			require.NoError(t, db.AutoMigrate(&BillingSchema{}))
			require.NoError(t, prepareBillingSchema(db))
			require.NoError(t, requireBillingSchema(db))
			require.NoError(t, prepareBillingSchema(db))
			// An empty marker alongside application data is not a fresh install.
			require.NoError(t, db.Delete(&BillingSchema{}, 1).Error)
			require.NoError(t, db.Table("users").AutoMigrate(&gatewayStartupLegacyUser{}))
			require.ErrorContains(t, prepareBillingSchema(db), "requires an empty database")
			var count int64
			require.NoError(t, db.Model(&BillingSchema{}).Count(&count).Error)
			assert.Zero(t, count)
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
		{&BillingRecovery{}, []string{"pre_consume_state", "token_required", "final_event_id", "version", "consumption_snapshot", "consumption_state", "consumption_statistics", "consumption_log", "consumption_error"}, []string{"billing_recovery_pending_scan", "idx_billing_recoveries_request_id"}},
		{&BillingEvent{}, []string{"event_id", "request_id", "operation", "available_delta", "reserved_delta", "used_delta", "fingerprint"}, []string{"idx_billing_events_event_id"}},
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
	require.NoError(t, db.Create(&recovery).Error)
	recovery.ID = 0
	assert.Error(t, db.Create(&recovery).Error, "logical request accounting must remain unique")
	adjustment := BillingEvent{EventID: "startup-recovery:settle", RequestID: "startup-recovery", Operation: BillingRecoveryOperationSettle, UsedDelta: 2}
	require.NoError(t, db.Create(&adjustment).Error)
	adjustment.ID = 0
	assert.Error(t, db.Create(&adjustment).Error, "an accounting effect cannot be duplicated")
}

func gatewayStartupDatabase(t *testing.T, kind common.DatabaseType, dsn string) func() (*gorm.DB, error) {
	t.Helper()
	config := &gorm.Config{Logger: &gatewayStartupMigrationRecorder{Interface: logger.Default.LogMode(logger.Silent)}}
	if kind == common.DatabaseTypeSQLite {
		path := filepath.Join(t.TempDir(), "gateway-startup.db")
		return func() (*gorm.DB, error) {
			db, err := openSQLiteDatabase(path)
			if err != nil {
				return nil, err
			}
			return db.Session(&gorm.Session{Logger: config.Logger}), nil
		}
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
		open = func() (*gorm.DB, error) {
			return gorm.Open(mysqlMigrationDialector{*mysql.Open(parsed.FormatDSN()).(*mysql.Dialector)}, config)
		}
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
			db, err := gorm.Open(postgresMigrationDialector{*postgres.New(postgres.Config{Conn: connection, PreferSimpleProtocol: true}).(*postgres.Dialector)}, config)
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

func TestGatewayStartupRejectsExistingDatabaseWithoutChanges(t *testing.T) {
	for _, test := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(test.kind), func(t *testing.T) {
			dsn := os.Getenv(test.env)
			if test.env != "" && dsn == "" {
				t.Skip(test.env + " is not configured; existing database rejection is unverified")
			}
			db, err := gatewayStartupDatabase(t, test.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousLogDB := DB, LOG_DB
			previousKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(test.kind, test.kind)
			initCol()
			connection, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousKind, previousLogKind)
				initCol()
				assert.NoError(t, connection.Close())
			})
			require.NoError(t, db.Table("users").AutoMigrate(&gatewayStartupLegacyUser{}))
			require.NoError(t, db.Table("tokens").AutoMigrate(&gatewayStartupLegacyToken{}))
			user := gatewayStartupLegacyUser{ID: 17, Username: "existing-user", Password: "test-only-password-hash", AffCode: "existing-aff", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "default"}
			token := gatewayStartupLegacyToken{ID: 23, UserID: 17, Key: "test-only-existing-key", Name: "existing-token", RemainQuota: 456, ModelLimits: "test-model"}
			require.NoError(t, db.Table("users").Create(&user).Error)
			require.NoError(t, db.Table("tokens").Create(&token).Error)
			tablesBefore, err := db.Migrator().GetTables()
			require.NoError(t, err)
			columnsBefore, err := db.Migrator().ColumnTypes("tokens")
			require.NoError(t, err)
			for range 2 {
				require.ErrorContains(t, requireBillingSchema(db), "not initialized")
				require.ErrorContains(t, migrateDB(), "requires an empty database")
				var restoredUser gatewayStartupLegacyUser
				var restoredToken gatewayStartupLegacyToken
				require.NoError(t, db.Table("users").First(&restoredUser, user.ID).Error)
				require.NoError(t, db.Table("tokens").First(&restoredToken, token.ID).Error)
				assert.Equal(t, user, restoredUser)
				assert.Equal(t, token, restoredToken)
				tablesAfter, err := db.Migrator().GetTables()
				require.NoError(t, err)
				assert.ElementsMatch(t, tablesBefore, tablesAfter)
				columnsAfter, err := db.Migrator().ColumnTypes("tokens")
				require.NoError(t, err)
				beforeNames, afterNames := []string{}, []string{}
				for _, column := range columnsBefore {
					beforeNames = append(beforeNames, column.Name())
				}
				for _, column := range columnsAfter {
					afterNames = append(afterNames, column.Name())
				}
				assert.ElementsMatch(t, beforeNames, afterNames)
				assert.False(t, db.Migrator().HasTable(&BillingSchema{}))
				assert.False(t, db.Migrator().HasTable(&BillingEvent{}))
				assert.False(t, db.Migrator().HasColumn(&Token{}, "available_quota"))
				assert.False(t, db.Migrator().HasColumn(&User{}, "reserved_quota"))
			}
		})
	}
}
