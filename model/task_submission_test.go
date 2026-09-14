package model

import (
	"context"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAcceptedTaskAndAccountingCommitTogether(t *testing.T) {
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " must target an isolated database")
			}
			db, err := gatewayStartupDatabase(t, database.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousLogDB, previousKind := DB, LOG_DB, common.MainDatabaseType()
			DB, LOG_DB = db, db
			common.SetMainDatabaseType(database.kind)
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetMainDatabaseType(previousKind)
				initCol()
				connection, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&Task{}, &User{}, &Token{}, &Channel{}, &Log{}, &BillingRecovery{}, &BillingRecoveryAdjustment{}))
			user := User{Username: "task-txn", Quota: 100}
			require.NoError(t, db.Create(&user).Error)
			token := Token{UserId: user.Id, Key: "isolated-task-txn", RemainQuota: 100}
			require.NoError(t, db.Create(&token).Error)
			channel := Channel{Name: "task-txn"}
			require.NoError(t, db.Create(&channel).Error)
			_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: "task-txn", UserID: user.Id, TokenID: token.Id, Source: "wallet", TokenRequired: true})
			require.NoError(t, err)
			require.NoError(t, PreConsumeWalletBillingRecovery("task-txn", 40, false))
			task := &Task{TaskID: "task_public", UserId: user.Id, ChannelId: channel.Id, Quota: 30, Status: TaskStatusNotStart,
				PrivateData: TaskPrivateData{UpstreamTaskID: "provider-private", TokenId: token.Id, SubmissionRequestID: "task-txn"}}
			consumption := BillingConsumption{Log: Log{UserId: user.Id, TokenId: token.Id, ChannelId: channel.Id, Quota: 30, ModelName: "task-test", Type: LogTypeConsume}, CountUsage: true, LogEnabled: true}
			require.NoError(t, db.Callback().Create().Before("gorm:create").Register("task_failure", func(tx *gorm.DB) {
				if tx.Statement.Table == "tasks" {
					tx.AddError(assert.AnError)
				}
			}))
			require.Error(t, PersistAcceptedTask(task, "task-txn", consumption))
			var recovery BillingRecovery
			require.NoError(t, db.First(&recovery, "request_id = ?", "task-txn").Error)
			assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
			assert.Empty(t, recovery.ConsumptionSnapshot)
			require.NoError(t, db.Callback().Create().Remove("task_failure"))
			require.NoError(t, PersistAcceptedTask(task, "task-txn", consumption))
			require.NoError(t, PersistAcceptedTask(task, "task-txn", consumption))
			var count int64
			require.NoError(t, db.Model(&Task{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			require.NoError(t, db.First(&recovery, "request_id = ?", "task-txn").Error)
			assert.Equal(t, BillingRecoveryStatusSettlementPending, recovery.Status)
			assert.ErrorIs(t, MarkBillingRecoveryRefundPending("task-txn", nil), ErrBillingRecoveryStateConflict)
			ready, err := TaskSubmissionReady(context.Background(), task)
			require.NoError(t, err)
			assert.False(t, ready)
			for range 2 {
				require.NoError(t, ApplyBillingRecoveryAdjustment("task-txn", BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
				require.NoError(t, ApplyBillingRecoveryAdjustment("task-txn", BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
				require.NoError(t, MarkBillingRecoverySettled("task-txn"))
				require.NoError(t, ApplyBillingRecoveryConsumption("task-txn"))
			}
			ready, err = TaskSubmissionReady(context.Background(), task)
			require.NoError(t, err)
			assert.True(t, ready)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 70, user.Quota)
			assert.Equal(t, 30, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			require.NoError(t, db.Model(&Log{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
		})
	}
}
