package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Each child exits without defers, exercising SQLite's real transaction recovery
// and durable intent rather than resetting an in-memory session object.
func TestBillingProcessExitRecovery(t *testing.T) {
	for _, stage := range []string{"reserve_transaction", "reserved", "dispatch", "intent", "funds", "log_insert"} {
		t.Run(stage, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "billing.db")
			binary, err := os.Executable()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			child := exec.CommandContext(ctx, binary, "-test.run=^TestBillingProcessExitWorker$", "-test.timeout=8s")
			child.Env = append(os.Environ(), "BILLING_EXIT_STAGE="+stage, "BILLING_EXIT_DATABASE="+path)
			output, err := child.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(output))
			require.Equal(t, 73, exit.ExitCode(), string(output))
			db := openBillingExitDatabase(t, path)
			for range 2 {
				_, err := recoverOneBillingRecovery(context.Background(), "crash")
				require.NoError(t, err)
			}
			var user model.User
			var token model.Token
			var recovery model.BillingRecovery
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&token, 1).Error)
			require.NoError(t, db.Where("request_id = ?", "crash").First(&recovery).Error)
			available, reserved, used := common.Quota(70), common.Quota(0), common.Quota(30)
			logs := int64(1)
			switch stage {
			case "reserve_transaction", "reserved":
				available, reserved, used, logs = 100, 0, 0, 0
			case "dispatch":
				available, reserved, used, logs = 80, 20, 0, 0
				assert.True(t, model.BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState))
			}
			assert.Equal(t, []common.Quota{available, reserved, used}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
			assert.Equal(t, []common.Quota{available, reserved, used}, []common.Quota{token.RemainQuota, token.ReservedQuota, token.UsedQuota})
			var count int64
			require.NoError(t, db.Model(&model.Log{}).Where("request_id = ?", "crash").Count(&count).Error)
			assert.Equal(t, logs, count, "a lost log acknowledgement must not duplicate the event")
			var events []model.BillingEvent
			require.NoError(t, db.Where("request_id = ?", "crash").Find(&events).Error)
			projection := []common.Quota{100, 0, 0}
			for _, event := range events {
				projection[0] += event.AvailableDelta
				projection[1] += event.ReservedDelta
				projection[2] += event.UsedDelta
			}
			assert.Equal(t, []common.Quota{available, reserved, used}, projection)
		})
	}
}

func openBillingExitDatabase(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	bindBillingIntegrationDatabases(t, db, db, common.DatabaseTypeSQLite)
	return db
}

func TestBillingProcessExitWorker(t *testing.T) {
	stage := os.Getenv("BILLING_EXIT_STAGE")
	if stage == "" {
		t.Skip("subprocess helper")
	}
	db := openBillingExitDatabase(t, os.Getenv("BILLING_EXIT_DATABASE"))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "crash", Quota: 100}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "crash-token", RemainQuota: 100}).Error)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: "crash", UserID: 1, TokenID: 1, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
	if stage == "reserve_transaction" {
		_ = model.BillingTransaction(context.Background(), func(tx *gorm.DB) error {
			require.NoError(t, model.ReserveTx(tx, "crash", 20))
			os.Exit(73)
			return nil
		})
	}
	require.NoError(t, model.Reserve("crash", 20))
	if stage == "reserved" {
		os.Exit(73)
	}
	quote := billingTestQuote(t, "p * 2")
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), "crash", "attempt-1", 0, quote))
	if stage == "dispatch" {
		os.Exit(73)
	}
	usage := &billing.Usage{InputTokens: 30, ContextTokens: 30, Source: "provider", Complete: true, Terminal: "completed"}
	charge, err := billing.Calculate(quote, *usage)
	require.NoError(t, err)
	require.NoError(t, model.PrepareBillingRecoveryConsumption("crash", model.BillingConsumption{
		Log:   model.Log{UserId: 1, TokenId: 1, Quota: charge.FinalQuota, ModelName: "test", CreatedAt: 7200},
		Usage: usage, Charge: charge, CountUsage: true, LogEnabled: true, ExportEnabled: true,
	}))
	if stage == "intent" {
		os.Exit(73)
	}
	require.NoError(t, model.Settle("crash", charge.FinalQuota))
	if stage == "funds" {
		os.Exit(73)
	}
	// The log committed; only its acknowledgement in the main DB is lost.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("crash_log_ack", func(tx *gorm.DB) {
		if tx.Statement.Table != "billing_recoveries" {
			return
		}
		updates, ok := tx.Statement.Dest.(map[string]any)
		if ok && updates["consumption_log"] == model.BillingRecoveryStateCompleted {
			os.Exit(73)
		}
	}))
	require.NoError(t, model.ApplyBillingRecoveryConsumption("crash"))
	t.Fatal("requested crash boundary was not reached")
}
