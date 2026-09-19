package service

import (
	"context"
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.User{},
		&model.Token{},
		&model.Log{},
		&model.QuotaData{},
		&model.Channel{},
		&model.Midjourney{},
		&model.TopUp{},
		&model.BillingRecovery{},
		&model.BillingEvent{},
		&model.BillingAttempt{}, &model.BillingPriceVersion{}, &model.BillingActivePrice{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM quota_data")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM billing_events")
		model.DB.Exec("DELETE FROM billing_recoveries")
		model.DB.Exec("DELETE FROM billing_attempts")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: common.Quota(quota), Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: common.Quota(remainQuota),
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func markBillingRecoveryStale(t *testing.T, requestID string) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).
		Where("request_id = ?", requestID).
		Update("updated_at", common.GetTimestamp()-3600).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     common.Quota(quota),
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource: billingSource,
			TokenId:       tokenId,
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "test-model",
			},
		},
	}
}

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("available_quota").Where("id = ?", id).First(&user).Error)
	return int(user.Quota)
}

func getUserUsageAccounting(t *testing.T, id int) (int, int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", id).First(&user).Error)
	return int(user.UsedQuota), user.RequestCount
}

func getChannelUsedQuota(t *testing.T, id int) int64 {
	t.Helper()
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&channel).Error)
	return int64(channel.UsedQuota)
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("available_quota").Where("id = ?", id).First(&token).Error)
	return int(token.RemainQuota)
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return int(token.UsedQuota)
}

func getTaskQuota(t *testing.T, id int64) int {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&task).Error)
	return int(task.Quota)
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

func completeTestTask(t *testing.T, ctx context.Context, task *model.Task, status model.TaskStatus, actualQuota int, reason string) error {
	t.Helper()
	if task.PrivateData.SubmissionRequestID == "" {
		acceptTestTask(t, task)
	}
	fromStatus := task.Status
	task.Status, task.Progress = status, "100%"
	_, err := completeTaskBilling(ctx, task, fromStatus, common.Quota(actualQuota), reason)
	return err
}

func acceptTestTask(t *testing.T, task *model.Task) {
	t.Helper()
	task.PrivateData.SubmissionRequestID = "accepted:" + task.TaskID
	if task.PrivateData.UpstreamTaskID == "" {
		task.PrivateData.UpstreamTaskID = "provider:" + task.TaskID
	}
	task.PrivateData.BillingSource = BillingSourceWallet
	quote, err := billing.NewQuote(billing.PriceVersion{ID: 1, Model: "test-model", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: decimal.NewFromInt(int64(task.Quota)).Div(decimal.NewFromInt(500000)).String()}}}, "1", nil, time.Now())
	require.NoError(t, err)
	task.PrivateData.BillingContext = &model.TaskBillingContext{Quote: quote, Usage: &billing.Usage{Quantities: map[string]string{"calls": "1"}, Source: "accepted_parameters", Complete: true}, OriginModelName: "test-model"}
	_, err = model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: task.PrivateData.SubmissionRequestID, UserID: task.UserId, TokenID: task.PrivateData.TokenId, TokenRequired: task.PrivateData.TokenId > 0, Source: BillingSourceWallet})
	require.NoError(t, err)
	require.NoError(t, model.Reserve(task.PrivateData.SubmissionRequestID, task.Quota))
	if task.ID > 0 {
		require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
	}
	require.NoError(t, model.PersistAcceptedTask(task, task.PrivateData.SubmissionRequestID, model.BillingConsumption{Log: model.Log{UserId: task.UserId, TokenId: task.PrivateData.TokenId, ChannelId: task.ChannelId, Quota: task.Quota}, CountUsage: true, LogEnabled: true}))
}

func TestTaskBillingAcceptedHoldAndSingleTerminalCharge(t *testing.T) {
	for _, actual := range []int{0, 20, 40, 140} {
		t.Run(decimal.NewFromInt(int64(actual)).String(), func(t *testing.T) {
			truncate(t)
			seedUser(t, 1, 100)
			seedToken(t, 1, 1, "task", 100)
			seedChannel(t, 1)
			task := makeTask(1, 1, 40, 1, BillingSourceWallet)
			acceptTestTask(t, task)
			assert.Equal(t, 60, getUserQuota(t, 1))
			used, count := getUserUsageAccounting(t, 1)
			assert.Zero(t, used)
			assert.Zero(t, count)
			assert.ErrorIs(t, model.MarkBillingRecoveryAutomaticRefundPending(task.PrivateData.SubmissionRequestID), model.ErrBillingRecoveryStateConflict)
			status := model.TaskStatus(model.TaskStatusSuccess)
			if actual == 0 {
				status = model.TaskStatusFailure
			}
			require.NoError(t, completeTestTask(t, context.Background(), task, status, actual, "confirmed terminal"))
			require.NoError(t, completeTestTask(t, context.Background(), task, status, actual, "duplicate terminal"))
			assert.Equal(t, 100-actual, getUserQuota(t, 1))
			assert.Equal(t, 100-actual, getTokenRemainQuota(t, 1))
			assert.Equal(t, actual, getTokenUsedQuota(t, 1))
			assert.EqualValues(t, actual, getChannelUsedQuota(t, 1))
			used, count = getUserUsageAccounting(t, 1)
			assert.Equal(t, actual, used)
			if actual == 0 {
				assert.Zero(t, count)
			} else {
				assert.Equal(t, 1, count)
			}
			var r model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&r).Error)
			assert.EqualValues(t, actual, r.FundingAmount+r.SettlementDelta)
			assert.EqualValues(t, 1, countLogs(t))
			assert.EqualValues(t, actual, getLastLog(t).Quota)
		})
	}
}

func TestTaskPriceUsesFrozenUsageInsteadOfAdaptorMoney(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 100)
	seedChannel(t, 1)
	task := makeTask(1, 1, 20, 0, BillingSourceWallet)
	acceptTestTask(t, task)
	task.Status = model.TaskStatusSuccess
	quota, _, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{TotalTokens: 999, CompletionTokens: 500})
	require.NoError(t, err)
	assert.EqualValues(t, 20, quota)
	require.NotNil(t, task.PrivateData.BillingContext.Charge)
	task.PrivateData.BillingContext.Quote = nil
	_, _, _, err = taskCompletionQuota(nil, task, nil)
	require.Error(t, err)
}
