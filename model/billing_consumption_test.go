package model

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupBillingConsumptionTest(t *testing.T) (*gorm.DB, *gorm.DB, BillingConsumption) {
	t.Helper()
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &QuotaData{}))
	logs, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logs.AutoMigrate(&Log{}))
	previousLogs := LOG_DB
	LOG_DB = logs
	t.Cleanup(func() { LOG_DB = previousLogs })
	require.NoError(t, db.Create(&User{Id: 1, Username: "consumption", Quota: 100}).Error)
	require.NoError(t, db.Create(&Token{Id: 2, UserId: 1, Key: "consumption-test", RemainQuota: 100}).Error)
	require.NoError(t, db.Create(&Channel{Id: 3, Name: "consumption"}).Error)
	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: "consume-once", UserID: 1, TokenID: 2, Source: "wallet", TokenRequired: true})
	require.NoError(t, err)
	require.NoError(t, PreConsumeWalletBillingRecovery("consume-once", 40, false))
	consumption := BillingConsumption{
		Log: Log{UserId: 1, TokenId: 2, ChannelId: 3, Username: "consumption", CreatedAt: 7200, Type: LogTypeConsume,
			ModelName: "test-model", Quota: 30, PromptTokens: 10, CompletionTokens: 20},
		CountUsage: true, LogEnabled: true, ExportEnabled: true, NodeName: "test-node",
	}
	require.NoError(t, PrepareBillingRecoveryConsumption("consume-once", consumption))
	return db, logs, consumption
}

func completeConsumptionFunds(t *testing.T) {
	t.Helper()
	require.NoError(t, ApplyBillingRecoveryAdjustment("consume-once", BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
	require.NoError(t, ApplyBillingRecoveryAdjustment("consume-once", BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
	require.NoError(t, MarkBillingRecoverySettled("consume-once"))
}

func TestBillingConsumptionPreservesSettlementIntentAndRejectsConflictingUsage(t *testing.T) {
	db, _, consumption := setupBillingConsumptionTest(t)
	assert.ErrorIs(t, ApplyBillingRecoveryConsumption("consume-once"), ErrBillingRecoveryStateConflict)
	assert.ErrorIs(t, MarkBillingRecoveryRefundPending("consume-once", nil), ErrBillingRecoveryStateConflict)
	require.NoError(t, PrepareBillingRecoveryConsumption("consume-once", consumption))
	consumption.Log.Quota++
	assert.ErrorIs(t, PrepareBillingRecoveryConsumption("consume-once", consumption), ErrBillingRecoveryStateConflict)
	assert.ErrorIs(t, UpdateBillingRecoverySettlement("consume-once", 999), ErrBillingRecoveryStateConflict)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, 60, user.Quota)
	assert.Zero(t, user.RequestCount)
}

func TestBillingConsumptionRecoversWithoutDuplicateCountersOrLogs(t *testing.T) {
	for _, failure := range []string{"statistics", "log_insert", "completion_marker"} {
		t.Run(failure, func(t *testing.T) {
			db, logs, _ := setupBillingConsumptionTest(t)
			completeConsumptionFunds(t)
			callbackName := "test:billing-consumption-" + failure
			switch failure {
			case "statistics":
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
					if tx.Statement.Table == "channels" {
						tx.AddError(assert.AnError)
					}
				}))
			case "log_insert":
				require.NoError(t, logs.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) { tx.AddError(assert.AnError) }))
			case "completion_marker":
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
					values, ok := tx.Statement.Dest.(map[string]any)
					if ok && values["consumption_state"] == BillingRecoveryStateCompleted {
						tx.AddError(assert.AnError)
					}
				}))
			}
			require.Error(t, ApplyBillingRecoveryConsumption("consume-once"))
			var pending BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "consume-once").First(&pending).Error)
			assert.Equal(t, BillingRecoveryStatusSettled, pending.Status)
			assert.Equal(t, BillingRecoveryStatePending, pending.ConsumptionState)
			assert.NotEmpty(t, pending.ConsumptionError)
			assert.ErrorIs(t, MarkBillingRecoveryRefundPending("consume-once", nil), ErrBillingRecoveryStateConflict)
			if failure == "log_insert" {
				require.NoError(t, logs.Callback().Create().Remove(callbackName))
			} else {
				require.NoError(t, db.Callback().Update().Remove(callbackName))
			}
			for i := 0; i < 2; i++ {
				require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
			}
			var user User
			var token Token
			var channel Channel
			var quotaData QuotaData
			var storedLogs []Log
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&token, 2).Error)
			require.NoError(t, db.First(&channel, 3).Error)
			require.NoError(t, db.First(&quotaData).Error)
			require.NoError(t, logs.Find(&storedLogs).Error)
			assert.Equal(t, 70, user.Quota)
			assert.Equal(t, 30, user.UsedQuota)
			assert.Equal(t, 1, user.RequestCount)
			assert.Equal(t, 70, token.RemainQuota)
			assert.Equal(t, 30, token.UsedQuota)
			assert.Equal(t, int64(30), channel.UsedQuota)
			assert.Equal(t, 30, quotaData.Quota)
			assert.Equal(t, 1, quotaData.Count)
			assert.Equal(t, 30, quotaData.TokenUsed)
			require.Len(t, storedLogs, 1)
			assert.Equal(t, "consume-once", storedLogs[0].RequestId)
			assert.Equal(t, 30, storedLogs[0].Quota)
			var complete BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "consume-once").First(&complete).Error)
			assert.Equal(t, BillingRecoveryStateCompleted, complete.ConsumptionState)
			assert.Empty(t, complete.ConsumptionError)
		})
	}
}

func TestBillingLogIdempotencyAllowsLegacyNullKeys(t *testing.T) {
	_, logs, _ := setupBillingConsumptionTest(t)
	require.NoError(t, logs.Create(&Log{Type: LogTypeSystem}).Error)
	require.NoError(t, logs.Create(&Log{Type: LogTypeSystem}).Error)
	completeConsumptionFunds(t)
	require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
	require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
	var count int64
	require.NoError(t, logs.Model(&Log{}).Count(&count).Error)
	assert.Equal(t, int64(3), count)
}

func TestMissingBillingConsumptionSnapshotRemainsVisibleForReconciliation(t *testing.T) {
	db, _, _ := setupBillingConsumptionTest(t)
	completeConsumptionFunds(t)
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", "consume-once").Updates(map[string]any{
		"consumption_snapshot": "", "consumption_state": "",
	}).Error)
	require.NoError(t, RecordBillingConsumptionPreparationFailure("consume-once", assert.AnError))
	assert.ErrorContains(t, ApplyBillingRecoveryConsumption("consume-once"), "reconciliation")
	rows, err := ListStaleBillingRecoveries(1<<62, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "unprepared", rows[0].ConsumptionState)
	assert.NotEmpty(t, rows[0].ConsumptionError)
	assert.Equal(t, BillingRecoveryStatusSettled, rows[0].Status)
}
