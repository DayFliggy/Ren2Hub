package model

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskCompletionRecoveryAcrossDatabases(t *testing.T) {
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
			previousDB, previousLogDB := DB, LOG_DB
			previousKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(database.kind, database.kind)
			common.RedisEnabled, common.BatchUpdateEnabled = false, false
			initCol()
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousKind, previousLogKind)
				common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
				initCol()
				connection, err := db.DB()
				require.NoError(t, err)
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&Task{}, &User{}, &Token{}, &Channel{}, &Log{}, &QuotaData{}, &BillingRecovery{}, &BillingRecoveryAdjustment{}))
			for _, actual := range []int{0, 15, 30, 60} {
				t.Run(fmt.Sprintf("wallet/%d", actual), func(t *testing.T) {
					testTaskCompletionRecovery(t, db, actual)
				})
			}
			for _, status := range []TaskStatus{TaskStatusSuccess, TaskStatusFailure} {
				t.Run("retired-source/"+string(status), func(t *testing.T) {
					testTaskCompletionRejectsRetiredSource(t, db, status)
				})
			}
		})
	}
}

func testTaskCompletionRejectsRetiredSource(t *testing.T, db *gorm.DB, status TaskStatus) {
	t.Helper()
	identity := "retired-" + string(status)
	user := User{Username: identity, AffCode: identity, Quota: 70, UsedQuota: 30, RequestCount: 1}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: identity, RemainQuota: 70, UsedQuota: 30}
	require.NoError(t, db.Create(&token).Error)
	channel := Channel{Name: identity, UsedQuota: 30}
	require.NoError(t, db.Create(&channel).Error)
	task := Task{TaskID: identity, UserId: user.Id, ChannelId: channel.Id, Quota: 30, Status: TaskStatusInProgress, Progress: "45%",
		PrivateData: TaskPrivateData{TokenId: token.Id, BillingSource: "subscription"}}
	require.NoError(t, db.Create(&task).Error)
	consumption := BillingConsumption{Log: Log{UserId: user.Id, TokenId: token.Id, ChannelId: channel.Id}, CountUsage: true, LogEnabled: true}
	task.Status, task.Progress = status, "100%"
	actual := 60
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
	assert.Equal(t, 30, persisted.Quota)
	assert.Equal(t, "subscription", persisted.PrivateData.BillingSource)
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.Equal(t, 70, user.Quota)
	assert.Equal(t, 30, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, 70, token.RemainQuota)
	assert.Equal(t, 30, token.UsedQuota)
	assert.EqualValues(t, 30, channel.UsedQuota)
	var count int64
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", TaskBillingRequestID(task.ID)).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&Log{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func testTaskCompletionRecovery(t *testing.T, db *gorm.DB, actual int) {
	t.Helper()
	ctx := context.Background()
	identity := fmt.Sprintf("wallet-%d", actual)
	user := User{Username: identity, AffCode: fmt.Sprintf("w%03d", actual), Quota: 70, UsedQuota: 30, RequestCount: 1}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: identity, RemainQuota: 70, UsedQuota: 30}
	require.NoError(t, db.Create(&token).Error)
	channel := Channel{Name: identity, UsedQuota: 30}
	require.NoError(t, db.Create(&channel).Error)
	task := Task{TaskID: identity, UserId: user.Id, ChannelId: channel.Id, Quota: 30, Status: TaskStatusInProgress,
		PrivateData: TaskPrivateData{TokenId: token.Id, BillingSource: "wallet", SubmissionRequestID: identity}}
	submission := BillingRecovery{RequestID: identity, UserID: user.Id, TokenID: token.Id, TokenRequired: true,
		Source: "wallet", Status: BillingRecoveryStatusSettled, ConsumptionState: BillingRecoveryStateCompleted}
	require.NoError(t, db.Create(&submission).Error)
	require.NoError(t, db.Create(&task).Error)
	oldStatus := task.Status
	task.Status, task.Progress = TaskStatusSuccess, "100%"
	task.Data = json.RawMessage(`{"status":"finished","result":"local-test"}`)
	task.PrivateData.ResultURL = "https://result.example.invalid/task"
	if actual == 0 {
		task.Status = TaskStatusFailure
	}
	consumption := BillingConsumption{Log: Log{UserId: user.Id, TokenId: token.Id, ChannelId: channel.Id,
		Username: identity, ModelName: "video", CreatedAt: 7200}, CountUsage: true, LogEnabled: true, ExportEnabled: true}
	requestID := TaskBillingRequestID(task.ID)

	// Neither half may survive a failure between intent creation and task CAS.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("completion_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(assert.AnError)
		}
	}))
	won, err := PersistTaskCompletion(ctx, &task, oldStatus, actual, consumption)
	require.Error(t, err)
	assert.False(t, won)
	require.NoError(t, db.Callback().Update().Remove("completion_failure"))
	var persisted Task
	require.NoError(t, db.First(&persisted, task.ID).Error)
	assert.Equal(t, oldStatus, persisted.Status)
	var count int64
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Count(&count).Error)
	assert.Zero(t, count)
	won, err = PersistTaskCompletion(ctx, &task, oldStatus, actual, consumption)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = PersistTaskCompletion(ctx, &task, oldStatus, actual+1, consumption)
	if task.Status == TaskStatusFailure {
		require.Error(t, err)
	} else {
		require.NoError(t, err)
	}
	assert.False(t, won)

	// Funding may complete before a token failure. Repeating it never credits
	// or debits again, including after the task is already terminal.
	require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
	if actual != 30 {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("token_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" {
				tx.AddError(assert.AnError)
			}
		}))
		require.Error(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
		require.NoError(t, db.Callback().Update().Remove("token_failure"))
	}
	require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
	require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
	require.NoError(t, MarkBillingRecoverySettled(requestID))
	if actual != 30 {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("statistics_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "channels" {
				tx.AddError(assert.AnError)
			}
		}))
		require.Error(t, ApplyBillingRecoveryConsumption(requestID))
		require.NoError(t, db.Callback().Update().Remove("statistics_failure"))
		require.NoError(t, db.First(&persisted, task.ID).Error)
		assert.Equal(t, 30, persisted.Quota)
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("log_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "logs" {
				tx.AddError(assert.AnError)
			}
		}))
		require.Error(t, ApplyBillingRecoveryConsumption(requestID))
		require.NoError(t, db.Callback().Create().Remove("log_failure"))
		require.NoError(t, db.First(&persisted, task.ID).Error)
		assert.Equal(t, actual, persisted.Quota)
	}
	for range 2 {
		require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
		require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
		require.NoError(t, ApplyBillingRecoveryConsumption(requestID))
	}
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, 100-actual, user.Quota)
	assert.Equal(t, actual, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	require.NoError(t, db.First(&token, token.Id).Error)
	assert.Equal(t, 100-actual, token.RemainQuota)
	assert.Equal(t, actual, token.UsedQuota)
	require.NoError(t, db.First(&channel, channel.Id).Error)
	assert.EqualValues(t, actual, channel.UsedQuota)
	require.NoError(t, db.First(&persisted, task.ID).Error)
	assert.Equal(t, actual, persisted.Quota)
	assert.JSONEq(t, string(task.Data), string(persisted.Data))
	assert.Equal(t, task.PrivateData.ResultURL, persisted.PrivateData.ResultURL)
	var logs []Log
	require.NoError(t, db.Where("request_id = ?", requestID).Find(&logs).Error)
	if actual == 30 {
		assert.Empty(t, logs)
	} else {
		require.Len(t, logs, 1)
		if actual < 30 {
			assert.Equal(t, LogTypeRefund, logs[0].Type)
			assert.Equal(t, 30-actual, logs[0].Quota)
		} else {
			assert.Equal(t, LogTypeConsume, logs[0].Type)
			assert.Equal(t, actual-30, logs[0].Quota)
		}
		var bucket QuotaData
		require.NoError(t, db.Where("user_id = ?", user.Id).First(&bucket).Error)
		assert.Zero(t, bucket.Count)
		assert.Equal(t, actual-30, bucket.Quota)
	}
}
