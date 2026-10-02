package model

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Each database runs identical transaction contracts; missing servers are explicit skips.
func withTaskBillingDatabases(t *testing.T, run func(*testing.T, *gorm.DB)) {
	t.Helper()
	t.Setenv("CRYPTO_SECRET", "billing-test-persistent-key")
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "TEST_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "TEST_POSTGRES_DSN"}} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if dsn == "" && database.env != "" {
				dsn = os.Getenv(strings.Replace(database.env, "TEST_", "GATEWAY_STARTUP_", 1))
			}
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " is not configured; database contract is unverified")
			}
			db, err := gatewayStartupDatabase(t, database.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousLogDB := DB, LOG_DB
			previousKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(database.kind, database.kind)
			common.RedisEnabled, common.BatchUpdateEnabled = false, false
			initCol()
			connection, err := db.DB()
			require.NoError(t, err)
			if database.kind == common.DatabaseTypeSQLite {
				connection.SetMaxOpenConns(1)
			}
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousKind, previousLogKind)
				common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
				initCol()
				assert.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&Task{}, &TaskResultEvent{}, &PerfMetric{}, &Midjourney{}, &User{}, &Token{}, &Channel{}, &Log{}, &QuotaData{}, &BillingRecovery{}, &BillingEvent{}, &BillingAttempt{}, &BillingPriceVersion{}))
			run(t, db)
		})
	}
}

func TestTaskCompletionRecoveryAcrossDatabases(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		for _, actual := range []common.Quota{0, 15, 30, 60, 150} {
			t.Run(fmt.Sprintf("wallet/%d", actual), func(t *testing.T) { testTaskCompletionRecovery(t, db, actual) })
		}
		for _, status := range []TaskStatus{TaskStatusSuccess, TaskStatusFailure} {
			t.Run("retired-source/"+string(status), func(t *testing.T) { testTaskCompletionRejectsRetiredSource(t, db, status) })
		}
	})
}

func TestTaskCompletionCannotBypassFinalBillingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*BillingConsumption)
	}{
		{"missing usage", func(c *BillingConsumption) { c.Usage = nil }},
		{"missing charge", func(c *BillingConsumption) { c.Charge = nil }},
		{"estimated", func(c *BillingConsumption) { c.Usage.Estimated = true }},
		{"partial", func(c *BillingConsumption) { c.Usage.Complete = false }},
		{"unknown terminal", func(c *BillingConsumption) { c.Usage.Terminal = "unknown" }},
		{"changed charge", func(c *BillingConsumption) { c.Charge.TotalUSD = "1" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupBillingRecoveryTestDB(t)
			require.NoError(t, db.AutoMigrate(&Task{}, &TaskResultEvent{}, &PerfMetric{}))
			user, token, _, task, consumption := newAcceptedTaskFixture(t, db, "task-final-guard")
			task.Status = TaskStatusSuccess
			tc.mutate(&consumption)
			won, err := PersistTaskCompletion(context.Background(), &task, TaskStatusInProgress, 30, consumption)
			require.Error(t, err)
			assert.False(t, won)
			var persisted Task
			require.NoError(t, db.First(&persisted, task.ID).Error)
			assert.Equal(t, TaskStatus(TaskStatusInProgress), persisted.Status)
			assertTaskAccountProjection(t, db, user.Id, token.Id, 60, 40, 0)
		})
	}
}

func newAcceptedTaskFixture(t *testing.T, db *gorm.DB, identity string, finalQuota ...common.Quota) (User, Token, Channel, Task, BillingConsumption) {
	t.Helper()
	user, token, channel, task, consumption := newTaskBillingFixture(t, db, identity, finalQuota...)
	require.NoError(t, PersistAcceptedTask(&task, identity, consumption))
	return user, token, channel, task, consumption
}

func newTaskBillingFixture(t *testing.T, db *gorm.DB, identity string, finalQuota ...common.Quota) (User, Token, Channel, Task, BillingConsumption) {
	t.Helper()
	user := User{Username: identity, AffCode: identity, Quota: 100}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: identity, RemainQuota: 100}
	require.NoError(t, db.Create(&token).Error)
	channel := Channel{Name: identity}
	require.NoError(t, db.Create(&channel).Error)
	task := Task{TaskID: identity, UserId: user.Id, ChannelId: channel.Id, Quota: 30, Status: TaskStatusInProgress, Progress: "45%", PrivateData: TaskPrivateData{UpstreamTaskID: "provider-" + identity, TokenId: token.Id, BillingSource: "wallet", SubmissionRequestID: identity}}
	consumption := BillingConsumption{Log: Log{UserId: user.Id, TokenId: token.Id, ChannelId: channel.Id, Quota: 30, Username: identity, ModelName: "video", CreatedAt: 7200}, CountUsage: true, LogEnabled: true, ExportEnabled: true}
	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: identity, UserID: user.Id, TokenID: token.Id, Source: "wallet", TokenRequired: true})
	require.NoError(t, err)
	require.NoError(t, Reserve(identity, 40))
	if len(finalQuota) > 0 {
		consumption.Log.Quota = finalQuota[0]
	}
	consumption, err = bindConsumptionEvidence(identity, consumption)
	require.NoError(t, err)
	consumption.Log.Quota = task.Quota
	return user, token, channel, task, consumption
}

func testTaskCompletionRejectsRetiredSource(t *testing.T, db *gorm.DB, status TaskStatus) {
	t.Helper()
	user, token, _, task, consumption := newAcceptedTaskFixture(t, db, "retired-"+string(status))
	task.PrivateData.BillingSource = "subscription"
	require.NoError(t, db.Model(&task).Update("private_data", task.PrivateData).Error)
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", task.PrivateData.SubmissionRequestID).Update("source", "subscription").Error)
	task.Status, task.Progress = status, "100%"
	actual := common.Quota(60)
	if status == TaskStatusFailure {
		actual = 0
	}
	for range 2 {
		won, err := PersistTaskCompletion(context.Background(), &task, TaskStatusInProgress, actual, consumption)
		require.ErrorIs(t, err, ErrUnsupportedBillingSource)
		assert.False(t, won)
	}
	var persisted Task
	require.NoError(t, db.First(&persisted, task.ID).Error)
	assert.Equal(t, TaskStatus(TaskStatusInProgress), persisted.Status)
	assert.Equal(t, "45%", persisted.Progress)
	assert.EqualValues(t, 30, persisted.Quota)
	assertTaskAccountProjection(t, db, user.Id, token.Id, 60, 40, 0)
	var count int64
	require.NoError(t, db.Model(&Log{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func assertTaskAccountProjection(t *testing.T, db *gorm.DB, userID, tokenID int, available, reserved, used common.Quota) {
	t.Helper()
	var user User
	require.NoError(t, db.First(&user, userID).Error)
	assert.Equal(t, available, user.Quota)
	assert.Equal(t, reserved, user.ReservedQuota)
	assert.Equal(t, used, user.UsedQuota)
	if tokenID > 0 {
		var token Token
		require.NoError(t, db.First(&token, tokenID).Error)
		assert.Equal(t, available, token.RemainQuota)
		assert.Equal(t, reserved, token.ReservedQuota)
		assert.Equal(t, used, token.UsedQuota)
	}
}

func completeTaskRecovery(t *testing.T, requestID string, actual common.Quota, release bool) {
	t.Helper()
	if release {
		require.NoError(t, Release(requestID))
	} else {
		require.NoError(t, Settle(requestID, actual))
	}
	require.NoError(t, ApplyBillingRecoveryConsumption(requestID))
}

func testTaskCompletionRecovery(t *testing.T, db *gorm.DB, actual common.Quota) {
	t.Helper()
	ctx := context.Background()
	user, token, channel, task, consumption := newAcceptedTaskFixture(t, db, fmt.Sprintf("wallet-%d", actual), actual)
	requestID := task.PrivateData.SubmissionRequestID
	oldStatus := task.Status
	task.Status, task.Progress = TaskStatusSuccess, "100%"
	task.Data = json.RawMessage(`{"status":"finished","result":"local-test"}`)
	task.PrivateData.ResultURL = "https://result.example.invalid/task"
	release := actual == 0
	if release {
		task.Status = TaskStatusFailure
	}
	// Persisted terminal evidence and billing intent have one commit boundary.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("completion_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(assert.AnError)
		}
	}))
	won, err := PersistTaskCompletion(ctx, &task, oldStatus, actual, consumption)
	require.ErrorIs(t, err, assert.AnError)
	assert.False(t, won)
	require.NoError(t, db.Callback().Update().Remove("completion_failure"))
	var persisted Task
	require.NoError(t, db.First(&persisted, task.ID).Error)
	assert.Equal(t, oldStatus, persisted.Status)
	var recovery BillingRecovery
	require.NoError(t, db.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
	assert.Equal(t, BillingRecoveryStateTaskPending, recovery.ConsumptionState)
	won, err = PersistTaskCompletion(ctx, &task, oldStatus, actual, consumption)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = PersistTaskCompletion(ctx, &task, oldStatus, actual, consumption)
	require.NoError(t, err)
	assert.False(t, won)
	// A Token write failure rolls back wallet, Token, event and completion marker together.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("token_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" {
			tx.AddError(assert.AnError)
		}
	}))
	if release {
		require.ErrorIs(t, Release(requestID), assert.AnError)
	} else {
		require.ErrorIs(t, Settle(requestID, actual), assert.AnError)
	}
	require.NoError(t, db.Callback().Update().Remove("token_failure"))
	assertTaskAccountProjection(t, db, user.Id, token.Id, 60, 40, 0)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Where("request_id = ? AND operation <> ?", requestID, "reserve").Count(&count).Error)
	assert.Zero(t, count)
	if release {
		require.NoError(t, Release(requestID))
	} else {
		require.NoError(t, Settle(requestID, actual))
	}
	// Fail after task quota projection so its update and statistics marker must roll back.
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("statistics_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "quota_data" {
			tx.AddError(assert.AnError)
		}
	}))
	require.ErrorIs(t, ApplyBillingRecoveryConsumption(requestID), assert.AnError)
	require.NoError(t, db.Callback().Create().Remove("statistics_failure"))
	require.NoError(t, db.First(&persisted, task.ID).Error)
	assert.EqualValues(t, 30, persisted.Quota)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("log_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			tx.AddError(assert.AnError)
		}
	}))
	require.ErrorIs(t, ApplyBillingRecoveryConsumption(requestID), assert.AnError)
	require.NoError(t, db.Callback().Create().Remove("log_failure"))
	require.NoError(t, db.First(&persisted, task.ID).Error)
	assert.Equal(t, actual, persisted.Quota)
	for range 2 {
		completeTaskRecovery(t, requestID, actual, release)
	}
	assertTaskAccountProjection(t, db, user.Id, token.Id, 100-actual, 0, actual)
	require.NoError(t, db.First(&user, user.Id).Error)
	expectedCount := 1
	if release {
		expectedCount = 0
	}
	assert.Equal(t, expectedCount, user.RequestCount)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, actual, channel.UsedQuota)
	require.NoError(t, db.First(&persisted, task.ID).Error)
	assert.Equal(t, actual, persisted.Quota)
	assert.JSONEq(t, string(task.Data), string(persisted.Data))
	assert.Equal(t, task.PrivateData.ResultURL, persisted.PrivateData.ResultURL)
	var logs []Log
	require.NoError(t, db.Where("request_id = ?", requestID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, actual, logs[0].Quota)
	expectedType := LogTypeConsume
	if release {
		expectedType = LogTypeRefund
	}
	assert.Equal(t, expectedType, logs[0].Type)
	var bucket QuotaData
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&bucket).Error)
	assert.Equal(t, actual, bucket.Quota)
	assert.Equal(t, expectedCount, bucket.Count, "exported request count matches committed usage statistics")
	require.NoError(t, db.Model(&BillingEvent{}).Where("request_id = ?", requestID).Count(&count).Error)
	assert.EqualValues(t, 2, count, "one reserve and one final event")
	require.NoError(t, db.Model(&BillingRecovery{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count, "completion reuses the original durable reservation")
}
