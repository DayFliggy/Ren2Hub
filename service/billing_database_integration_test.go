package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// External DSNs only bootstrap disposable databases. No table in the supplied
// database is migrated, truncated, or deleted by this suite.
func TestBillingDatabaseIntegration(t *testing.T) {
	for _, backend := range []struct {
		name string
		env  string
		kind common.DatabaseType
	}{
		{name: "sqlite", kind: common.DatabaseTypeSQLite},
		{name: "mysql", env: "BILLING_TEST_MYSQL_DSN", kind: common.DatabaseTypeMySQL},
		{name: "postgres", env: "BILLING_TEST_POSTGRES_DSN", kind: common.DatabaseTypePostgreSQL},
	} {
		t.Run(backend.name, func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " is not configured")
			}
			mainDB, logDB := isolatedBillingDatabases(t, backend.kind, dsn)
			bindBillingIntegrationDatabases(t, mainDB, logDB, backend.kind)
			for index, scenario := range append([]string{
				"duplicate_finalization", "funding_failure", "funding_marker_failure", "token_failure",
				"statistics_failure", "log_insert_failure", "log_completion_failure", "canceled_request",
				"playground_token_identity", "free_consumption", "concurrent_delivery", "refund_before_settlement",
				"zero_precharge_refund", "zero_precharge_refund_completion_failure",
				"pricing_reconciliation",
			}, billingPricingGuardScenarios...) {
				t.Run(scenario, func(t *testing.T) {
					c, info, cancel := seedBillingIntegrationRequest(t, index+1, scenario)
					for _, guardedScenario := range billingPricingGuardScenarios {
						if scenario == guardedScenario {
							runBillingPricingGuardScenario(t, c, info, scenario)
							return
						}
					}
					if scenario == "pricing_reconciliation" {
						info.TieredBillingSnapshot = makeSnapshot(`c == 13 ? float("invalid-price") : p * 2 + c * 10`, 1, 10, 1)
						require.Nil(t, PrepareBillingForSelectedModel(c, info))
						usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 13, TotalTokens: 23}
						cancel()
						PostTextConsumeQuota(c, info, usage, nil)
						require.Error(t, info.BillingSettlementError)
						PostTextConsumeQuota(c, info, usage, nil)
						require.Error(t, info.BillingSettlementError)
						info.Billing.Refund(c)
						status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
						require.NoError(t, err)
						assert.Equal(t, model.BillingRecoveryStatusReconciliation, status)
						assert.ErrorIs(t, model.MarkBillingRecoveryRefundPending(info.RequestId, nil), model.ErrBillingRecoveryStateConflict)
						assert.ErrorIs(t, model.UpdateBillingRecoverySettlement(info.RequestId, 0), model.ErrBillingRecoveryStateConflict)
						var recovery model.BillingRecovery
						require.NoError(t, mainDB.Where("request_id = ?", info.RequestId).First(&recovery).Error)
						var failure model.BillingPricingFailure
						require.NoError(t, common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &failure))
						assert.Equal(t, billingexpr.TokenParams{P: 10, C: 13, Len: 10}, failure.Tokens)
						assert.Equal(t, info.TieredBillingSnapshot.ExprHash, failure.Snapshot.ExprHash)
						assertBillingIntegrationBalances(t, info, 95, 95, 0, 5, 0, 0)
						return
					}
					if scenario == "zero_precharge_refund" || scenario == "zero_precharge_refund_completion_failure" {
						session := info.Billing.(*BillingSession)
						require.Zero(t, session.GetPreConsumedQuota())
						require.True(t, session.NeedsRefund())
						removeFailure := injectBillingIntegrationFailure(t, mainDB, logDB, scenario)
						t.Cleanup(func() {
							if removeFailure != nil {
								removeFailure()
							}
						})
						session.Refund(c)
						if removeFailure != nil {
							require.Error(t, info.BillingRefundError)
							assert.True(t, session.NeedsRefund())
							removeFailure()
							removeFailure = nil
						}
						session.Refund(c)
						session.Refund(c)
						require.NoError(t, info.BillingRefundError)
						assert.False(t, session.NeedsRefund())
						select {
						case <-session.recoveryDone:
						default:
							t.Error("zero-precharge refund did not stop its heartbeat")
						}
						var recovery model.BillingRecovery
						require.NoError(t, mainDB.Where("request_id = ?", info.RequestId).First(&recovery).Error)
						assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
						assert.True(t, model.BillingRecoveryRefundComplete(recovery))
						assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
						return
					}
					source := "estimated"
					if index%2 == 0 {
						source = "provider_and_estimated"
					}
					info.CaptureResponseUsage(&dto.Usage{PromptTokens: 17, CompletionTokens: 3, TotalTokens: 20, UsageSource: source}, "text", nil)
					quota := 20
					if scenario == "free_consumption" {
						quota = 0
					}
					params := model.RecordConsumeLogParams{TokenId: info.TokenId, ChannelId: info.ChannelId, Quota: quota,
						PromptTokens: 17, CompletionTokens: 3, ModelName: "billing-integration", TokenName: "integration",
						Group: "default", Other: map[string]interface{}{"billing_source": "wallet"}}
					if scenario == "refund_before_settlement" {
						info.Billing.Refund(c)
						assert.Error(t, info.Billing.Settle(quota))
						assertBillingIntegrationBalances(t, info, 100, 100, 0, 0, 0, 0)
						return
					}
					if scenario == "canceled_request" {
						cancel()
					}
					removeFailure := injectBillingIntegrationFailure(t, mainDB, logDB, scenario)
					settleBillingConsumption(c, info, params, true)
					if removeFailure != nil {
						require.Error(t, info.BillingSettlementError)
						info.Billing.Refund(c)
						var state model.BillingRecovery
						require.NoError(t, mainDB.Where("request_id = ?", info.RequestId).First(&state).Error)
						assert.NotEqual(t, model.BillingRecoveryStatusRefunded, state.Status)
						assert.NotEmpty(t, state.ConsumptionSnapshot)
						if scenario == "funding_failure" || scenario == "funding_marker_failure" {
							assertBillingIntegrationBalances(t, info, 95, 95, 0, 5, 0, 0)
						} else if scenario == "token_failure" {
							assertBillingIntegrationBalances(t, info, 80, 95, 0, 5, 0, 0)
						}
						removeFailure()
						status, err := recoverOneBillingRecovery(context.Background(), info.RequestId)
						require.NoError(t, err)
						assert.Equal(t, model.BillingRecoveryStatusSettled, status)
					} else {
						require.NoError(t, info.BillingSettlementError)
					}
					settleBillingConsumption(c, info, params, true)
					require.NoError(t, info.BillingSettlementError)
					info.Billing.Refund(c)
					if scenario == "concurrent_delivery" {
						var workers sync.WaitGroup
						errors := make(chan error, 2)
						workers.Add(2)
						for i := 0; i < 2; i++ {
							go func() { defer workers.Done(); errors <- model.ApplyBillingRecoveryConsumption(info.RequestId) }()
						}
						workers.Wait()
						close(errors)
						for err := range errors {
							require.NoError(t, err)
						}
					}
					tokenQuota, tokenUsed := 100-quota, quota
					if info.IsPlayground {
						tokenQuota, tokenUsed = 100, 0
					}
					assertBillingIntegrationBalances(t, info, 100-quota, tokenQuota, quota, tokenUsed, 1, 1)
					var bucket model.QuotaData
					require.NoError(t, mainDB.Where("user_id = ?", info.UserId).First(&bucket).Error)
					assert.Equal(t, quota, bucket.Quota)
					assert.Equal(t, 20, bucket.TokenUsed)
					assert.Equal(t, 1, bucket.Count)
					var complete model.BillingRecovery
					require.NoError(t, mainDB.Where("request_id = ?", info.RequestId).First(&complete).Error)
					assert.Equal(t, model.BillingRecoveryStateCompleted, complete.ConsumptionState)
					assert.Empty(t, complete.ConsumptionError)
					var log model.Log
					require.NoError(t, logDB.Where("request_id = ?", info.RequestId).First(&log).Error)
					var other map[string]any
					require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
					assert.Equal(t, source, other["usage_source"], "recovery must retain the original usage evidence")
				})
			}
		})
	}
}

func isolatedBillingDatabases(t *testing.T, kind common.DatabaseType, baseDSN string) (*gorm.DB, *gorm.DB) {
	t.Helper()
	config := &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}
	if kind == common.DatabaseTypeSQLite {
		var dbs []*gorm.DB
		for _, name := range []string{"main", "logs"} {
			db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), name+".db")), config)
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
			dbs = append(dbs, db)
		}
		t.Log("SQLite: isolated main and log files")
		return dbs[0], dbs[1]
	}
	open := func(database string) *gorm.DB {
		var conn *sql.DB
		var dialect gorm.Dialector
		if kind == common.DatabaseTypeMySQL {
			parsed, err := mysqldriver.ParseDSN(baseDSN)
			require.NoError(t, err)
			if database != "" {
				parsed.DBName = database
			}
			parsed.ParseTime, parsed.Timeout, parsed.ReadTimeout, parsed.WriteTimeout = true, 3*time.Second, 5*time.Second, 5*time.Second
			conn, err = sql.Open("mysql", parsed.FormatDSN())
			require.NoError(t, err)
			dialect = mysql.New(mysql.Config{Conn: conn})
		} else {
			parsed, err := pgx.ParseConfig(baseDSN)
			require.NoError(t, err)
			if database != "" {
				parsed.Database = database
			}
			parsed.ConnectTimeout = 3 * time.Second
			parsed.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
			parsed.RuntimeParams["statement_timeout"] = "5000"
			conn = stdlib.OpenDB(*parsed)
			dialect = postgres.New(postgres.Config{Conn: conn, PreferSimpleProtocol: true})
		}
		conn.SetMaxOpenConns(4)
		db, err := gorm.Open(dialect, config)
		if err != nil {
			_ = conn.Close()
		}
		require.NoError(t, err)
		return db
	}
	admin := open("")
	adminConn, err := admin.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, adminConn.Close()) })
	var version string
	require.NoError(t, admin.Raw("SELECT VERSION()").Scan(&version).Error)
	t.Logf("Database server: %s", version)
	var dbs []*gorm.DB
	for _, suffix := range []string{"main", "logs"} {
		name := fmt.Sprintf("billing_validation_%d_%s", time.Now().UnixNano(), suffix)
		require.True(t, regexp.MustCompile(`^billing_validation_[0-9]+_(main|logs)$`).MatchString(name))
		createDatabase := "CREATE DATABASE " + name
		if kind == common.DatabaseTypeMySQL {
			createDatabase += " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
		}
		require.NoError(t, admin.Exec(createDatabase).Error)
		t.Logf("Disposable database: %s", name)
		db := open(name)
		conn, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, conn.Close())
			require.NoError(t, admin.Exec("DROP DATABASE "+name).Error)
		})
		dbs = append(dbs, db)
	}
	return dbs[0], dbs[1]
}

func bindBillingIntegrationDatabases(t *testing.T, mainDB, logDB *gorm.DB, kind common.DatabaseType) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch, previousLog, previousExport := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled
	model.DB, model.LOG_DB = mainDB, logDB
	common.SetDatabaseTypes(kind, kind)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = false, false, true, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMainType, previousLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled, common.DataExportEnabled = previousRedis, previousBatch, previousLog, previousExport
	})
	require.NoError(t, mainDB.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.QuotaData{}, &model.BillingRecovery{}, &model.BillingRecoveryAdjustment{}))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	// Existing non-consumption logs retain NULL idempotency keys after migration.
	require.NoError(t, logDB.Create(&model.Log{Type: model.LogTypeSystem}).Error)
	require.NoError(t, logDB.Create(&model.Log{Type: model.LogTypeSystem}).Error)
}

func seedBillingIntegrationRequest(t *testing.T, id int, scenario string) (*gin.Context, *relaycommon.RelayInfo, context.CancelFunc) {
	t.Helper()
	require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("billing%d", id), AffCode: fmt.Sprintf("test%d", id), Quota: 100}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: id, UserId: id, Key: fmt.Sprintf("billing-integration-token-%d", id), RemainQuota: 100}).Error)
	require.NoError(t, model.DB.Create(&model.Channel{Id: id, Name: fmt.Sprintf("billing%d", id)}).Error)
	ctx, cancel := context.WithCancel(context.Background())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{RequestId: fmt.Sprintf("billing-%d", id), UserId: id, TokenId: id,
		TokenKey: fmt.Sprintf("billing-integration-token-%d", id), ForcePreConsume: true, StartTime: time.Now(),
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"}, IsPlayground: scenario == "playground_token_identity",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: id},
	}
	precharge := 5
	if scenario == "zero_precharge_refund" || scenario == "zero_precharge_refund_completion_failure" {
		precharge = 0
	}
	apiErr := PreConsumeBilling(c, precharge, info)
	require.Nil(t, apiErr, "%v", apiErr)
	require.NoError(t, model.TouchBillingRecovery(info.RequestId))
	require.NoError(t, model.TouchBillingRecovery(info.RequestId))
	t.Cleanup(func() { cancel(); info.Billing.(*BillingSession).stopRecoveryHeartbeat() })
	return c, info, cancel
}

func injectBillingIntegrationFailure(t *testing.T, mainDB, logDB *gorm.DB, scenario string) func() {
	t.Helper()
	const callback = "test:billing-failure"
	switch scenario {
	case "funding_failure", "token_failure", "statistics_failure", "log_completion_failure", "zero_precharge_refund_completion_failure":
		require.NoError(t, mainDB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
			values, _ := tx.Statement.Dest.(map[string]interface{})
			_, balanceChange := values["quota"]
			if scenario == "funding_failure" && tx.Statement.Table == "users" && balanceChange ||
				scenario == "token_failure" && tx.Statement.Table == "tokens" ||
				scenario == "statistics_failure" && tx.Statement.Table == "channels" ||
				scenario == "log_completion_failure" && values["consumption_state"] == model.BillingRecoveryStateCompleted ||
				scenario == "zero_precharge_refund_completion_failure" && tx.Statement.Table == "billing_recoveries" && values["status"] == model.BillingRecoveryStatusRefunded {
				tx.AddError(assert.AnError)
			}
		}))
		return func() { require.NoError(t, mainDB.Callback().Update().Remove(callback)) }
	case "funding_marker_failure":
		require.NoError(t, mainDB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if marker, ok := tx.Statement.Dest.(*model.BillingRecoveryAdjustment); ok && marker.Component == model.BillingRecoveryComponentFunding {
				tx.AddError(assert.AnError)
			}
		}))
		return func() { require.NoError(t, mainDB.Callback().Create().Remove(callback)) }
	case "log_insert_failure":
		require.NoError(t, logDB.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) { tx.AddError(assert.AnError) }))
		return func() { require.NoError(t, logDB.Callback().Create().Remove(callback)) }
	}
	return nil
}

func assertBillingIntegrationBalances(t *testing.T, info *relaycommon.RelayInfo, wallet, tokenQuota, used, tokenUsed, count, logs int) {
	t.Helper()
	var user model.User
	var token model.Token
	var channel model.Channel
	var logCount int64
	require.NoError(t, model.DB.First(&user, info.UserId).Error)
	require.NoError(t, model.DB.First(&token, info.TokenId).Error)
	require.NoError(t, model.DB.First(&channel, info.ChannelId).Error)
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", info.RequestId).Count(&logCount).Error)
	assert.Equal(t, wallet, user.Quota)
	assert.Equal(t, tokenQuota, token.RemainQuota)
	assert.Equal(t, used, user.UsedQuota)
	assert.Equal(t, tokenUsed, token.UsedQuota)
	assert.Equal(t, count, user.RequestCount)
	assert.Equal(t, int64(used), channel.UsedQuota)
	assert.Equal(t, int64(logs), logCount)
}
