package service

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
		&model.UserSubscription{},
		&model.BillingRecovery{},
		&model.BillingRecoveryAdjustment{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Seed helpers
// ---------------------------------------------------------------------------

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
		model.DB.Exec("DELETE FROM user_subscriptions")
		model.DB.Exec("DELETE FROM billing_recovery_adjustments")
		model.DB.Exec("DELETE FROM billing_recoveries")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: quota, Status: common.UserStatusEnabled}
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
		RemainQuota: remainQuota,
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedSubscription(t *testing.T, id int, userId int, amountTotal int64, amountUsed int64) {
	t.Helper()
	sub := &model.UserSubscription{
		Id:          id,
		UserId:      userId,
		AmountTotal: amountTotal,
		AmountUsed:  amountUsed,
		Status:      "active",
		StartTime:   time.Now().Unix(),
		EndTime:     time.Now().Add(30 * 24 * time.Hour).Unix(),
	}
	require.NoError(t, model.DB.Create(sub).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func seedChargedAccounting(t *testing.T, userID, channelID, tokenID, quota, requestCount int) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Updates(map[string]any{
		"used_quota":    quota,
		"request_count": requestCount,
	}).Error)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channelID).
		Update("used_quota", quota).Error)
	if tokenID > 0 {
		require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", tokenID).
			Update("used_quota", quota).Error)
	}
}

func markBillingRecoveryStale(t *testing.T, requestID string) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).
		Where("request_id = ?", requestID).
		Update("updated_at", common.GetTimestamp()-3600).Error)
}

func TestRecoverStaleBillingRecoveryRefundIsIdempotent(t *testing.T) {
	truncate(t)
	const userID = 710
	const requestID = "billing-recovery-refund-test"
	seedUser(t, userID, 60)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: requestID,
		UserID:    userID,
		TokenID:   0,
		Source:    BillingSourceWallet,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 0))
	markBillingRecoveryStale(t, requestID)

	summary, err := RecoverStaleBillingRecoveries(context.Background(), common.GetTimestamp()-30, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Scanned)
	assert.Equal(t, 1, summary.Refunded)

	var quota int
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Pluck("quota", &quota).Error)
	assert.Equal(t, 100, quota)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
	var adjustments int64
	require.NoError(t, model.DB.Model(&model.BillingRecoveryAdjustment{}).
		Where("request_id = ?", requestID).Count(&adjustments).Error)
	assert.Equal(t, int64(1), adjustments)

	summary, err = RecoverStaleBillingRecoveries(context.Background(), common.GetTimestamp()-30, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.Scanned)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Pluck("quota", &quota).Error)
	assert.Equal(t, 100, quota)
}

func TestRecoverStaleBillingRecoverySettlementResumesAndIsIdempotent(t *testing.T) {
	truncate(t)
	const userID = 711
	const requestID = "billing-recovery-settlement-test"
	seedUser(t, userID, 60)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: requestID,
		UserID:    userID,
		TokenID:   0,
		Source:    BillingSourceWallet,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 0))
	require.NoError(t, model.UpdateBillingRecoverySettlement(requestID, 10))
	markBillingRecoveryStale(t, requestID)

	summary, err := RecoverStaleBillingRecoveries(context.Background(), common.GetTimestamp()-30, 10)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Scanned)
	assert.Equal(t, 1, summary.Settled)

	var quota int
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Pluck("quota", &quota).Error)
	assert.Equal(t, 50, quota)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
	assert.True(t, model.BillingRecoverySettlementComplete(recovery))

	summary, err = RecoverStaleBillingRecoveries(context.Background(), common.GetTimestamp()-30, 10)
	require.NoError(t, err)
	assert.Equal(t, 0, summary.Scanned)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Pluck("quota", &quota).Error)
	assert.Equal(t, 50, quota)
}

func TestBillingRecoverySettlementAlreadyCompletedIsIdempotent(t *testing.T) {
	truncate(t)
	const userID = 716
	const requestID = "billing-recovery-settled-race-test"
	seedUser(t, userID, 100)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: requestID,
		UserID:    userID,
		Source:    BillingSourceWallet,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 0))
	require.NoError(t, model.UpdateBillingRecoverySettlement(requestID, 10))
	require.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationSettle))
	require.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationSettle))
	require.NoError(t, model.MarkBillingRecoverySettled(requestID))

	// A late online Settle must not overwrite the settled delta or report a
	// false state conflict after the recovery worker completed the request.
	require.NoError(t, model.UpdateBillingRecoverySettlement(requestID, 999))
	require.NoError(t, model.MarkBillingRecoverySettled(requestID))

	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, int64(10), recovery.SettlementDelta)
	assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
}

func TestBillingRecoveryStateFencePreventsSettlementRefundRace(t *testing.T) {
	truncate(t)
	const userID = 712
	const requestID = "billing-recovery-state-fence-test"
	seedUser(t, userID, 100)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: requestID,
		UserID:    userID,
		TokenID:   0,
		Source:    BillingSourceWallet,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 0))
	require.NoError(t, model.UpdateBillingRecoverySettlement(requestID, 10))

	err = model.MarkBillingRecoveryRefundPending(requestID, nil)
	assert.ErrorIs(t, err, model.ErrBillingRecoveryStateConflict)
	require.NoError(t, model.UpdateBillingRecoverySettlement(requestID, 10))

	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettlementPending, recovery.Status)
}

func TestBillingRecoveryRefundFencePreventsSettlementAfterRefundStarts(t *testing.T) {
	truncate(t)
	const userID = 713
	const requestID = "billing-recovery-refund-fence-test"
	seedUser(t, userID, 60)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: requestID,
		UserID:    userID,
		TokenID:   0,
		Source:    BillingSourceWallet,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 0))
	require.NoError(t, model.MarkBillingRecoveryRefundPending(requestID, nil))

	assert.ErrorIs(t, model.UpdateBillingRecoverySettlement(requestID, 10), model.ErrBillingRecoveryStateConflict)
	require.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationRefund))
	require.NoError(t, model.MarkBillingRecoveryRefunded(requestID, nil))

	var quota int
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Pluck("quota", &quota).Error)
	assert.Equal(t, 100, quota)
	assert.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationRefund))
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", userID).Pluck("quota", &quota).Error)
	assert.Equal(t, 100, quota)
}

func TestBillingRecoveryPlaygroundTokenSettlementCreatesMarkerWithoutTokenLookup(t *testing.T) {
	truncate(t)
	const userID = 714
	const requestID = "billing-recovery-playground-token-marker-test"
	seedUser(t, userID, 60)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID: requestID,
		UserID:    userID,
		TokenID:   0,
		Source:    BillingSourceWallet,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 0))
	require.NoError(t, model.UpdateBillingRecoverySettlement(requestID, 10))
	require.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationSettle))
	require.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationSettle))

	var marker model.BillingRecoveryAdjustment
	require.NoError(t, model.DB.Where("request_id = ? AND component = ? AND operation = ?", requestID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationSettle).First(&marker).Error)
	assert.Zero(t, marker.Amount)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.True(t, model.BillingRecoverySettlementComplete(recovery))
}

func TestBillingRecoveryTokenSettlementUsesDeltaNotPreConsumedAmount(t *testing.T) {
	truncate(t)
	const userID = 715
	const tokenID = 7151
	const requestID = "billing-recovery-token-delta-test"
	seedUser(t, userID, 100)
	seedToken(t, tokenID, userID, "token-delta-test-key", 60)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{
		RequestID:     requestID,
		UserID:        userID,
		TokenID:       tokenID,
		Source:        BillingSourceWallet,
		TokenAmount:   40,
		TokenRequired: true,
	})
	require.NoError(t, err)
	require.NoError(t, model.UpdateBillingRecoveryAmounts(requestID, 0, 40, 0, 40))
	require.NoError(t, model.UpdateBillingRecoverySettlement(requestID, 10))
	require.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentFunding, model.BillingRecoveryOperationSettle))
	require.NoError(t, model.ApplyBillingRecoveryAdjustment(requestID, model.BillingRecoveryComponentToken, model.BillingRecoveryOperationSettle))

	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota", "used_quota").Where("id = ?", tokenID).First(&token).Error)
	assert.Equal(t, 50, token.RemainQuota)
	assert.Equal(t, 10, token.UsedQuota)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string, subscriptionId int) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     quota,
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource:  billingSource,
			SubscriptionId: subscriptionId,
			TokenId:        tokenId,
			BillingContext: &model.TaskBillingContext{
				ModelPrice:      0.02,
				GroupRatio:      1.0,
				OriginModelName: "test-model",
			},
		},
	}
}

func TestPriceDataOtherRatiosFilterAndSnapshot(t *testing.T) {
	priceData := types.PriceData{}

	priceData.AddOtherRatio("zero", 0)
	priceData.AddOtherRatio("negative", -0.5)
	priceData.AddOtherRatio("nan", math.NaN())
	priceData.AddOtherRatio("inf", math.Inf(1))
	priceData.AddOtherRatio("one", 1)
	priceData.AddOtherRatio("positive", 2.5)

	ratios := priceData.OtherRatios()
	require.Len(t, ratios, 2)
	assert.Equal(t, 1.0, ratios["one"])
	assert.Equal(t, 2.5, ratios["positive"])
	assert.True(t, priceData.HasOtherRatio("one"))
	assert.False(t, priceData.HasOtherRatio("zero"))

	ratios["positive"] = 99
	ratios["new"] = 3
	nextSnapshot := priceData.OtherRatios()
	assert.Equal(t, 2.5, nextSnapshot["positive"])
	assert.NotContains(t, nextSnapshot, "new")
}

func TestPriceDataReplaceAndApplyOtherRatios(t *testing.T) {
	priceData := types.PriceData{}

	replaced := priceData.ReplaceOtherRatios(map[string]float64{
		"zero":     0,
		"negative": -3,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
		"one":      1,
		"duration": 2,
		"size":     1.5,
	})

	require.True(t, replaced)
	assert.Equal(t, 3.0, priceData.OtherRatioMultiplier())
	assert.Equal(t, 30.0, priceData.ApplyOtherRatiosToFloat(10))
	assert.Equal(t, 10.0, priceData.RemoveOtherRatiosFromFloat(30))
	assert.True(t, decimal.NewFromInt(30).Equal(priceData.ApplyOtherRatiosToDecimal(decimal.NewFromInt(10))))

	replaced = priceData.ReplaceOtherRatios(map[string]float64{
		"zero": 0,
		"nan":  math.NaN(),
	})

	require.False(t, replaced)
	assert.Nil(t, priceData.OtherRatios())
	assert.Equal(t, 1.0, priceData.OtherRatioMultiplier())
}

func TestTaskBillingOtherFiltersHistoricalOtherRatios(t *testing.T) {
	task := makeTask(1, 1, 100, 0, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.OtherRatios = map[string]float64{
		"seconds":  2,
		"identity": 1,
		"zero":     0,
		"negative": -1,
		"nan":      math.NaN(),
		"inf":      math.Inf(1),
	}

	other := taskBillingOther(task)

	assert.Equal(t, 2.0, other["seconds"])
	assert.Equal(t, 1.0, other["identity"])
	assert.NotContains(t, other, "zero")
	assert.NotContains(t, other, "negative")
	assert.NotContains(t, other, "nan")
	assert.NotContains(t, other, "inf")
}

func TestTaskBillingContextPriceDataFiltersMultiplier(t *testing.T) {
	priceData := taskBillingContextPriceData(&model.TaskBillingContext{
		OtherRatios: map[string]float64{
			"seconds":  2,
			"size":     3,
			"identity": 1,
			"zero":     0,
			"negative": -1,
			"nan":      math.NaN(),
			"inf":      math.Inf(1),
		},
	})

	require.NotNil(t, priceData)
	assert.Equal(t, 6.0, priceData.OtherRatioMultiplier())
	assert.Equal(t, map[string]float64{
		"seconds":  2,
		"size":     3,
		"identity": 1,
	}, priceData.OtherRatios())
}

// ---------------------------------------------------------------------------
// Read-back helpers
// ---------------------------------------------------------------------------

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&user).Error)
	return user.Quota
}

func getUserUsageAccounting(t *testing.T, id int) (int, int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", id).First(&user).Error)
	return user.UsedQuota, user.RequestCount
}

func getChannelUsedQuota(t *testing.T, id int) int64 {
	t.Helper()
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&channel).Error)
	return channel.UsedQuota
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("remain_quota").Where("id = ?", id).First(&token).Error)
	return token.RemainQuota
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return token.UsedQuota
}

func getSubscriptionUsed(t *testing.T, id int) int64 {
	t.Helper()
	var sub model.UserSubscription
	require.NoError(t, model.DB.Select("amount_used").Where("id = ?", id).First(&sub).Error)
	return sub.AmountUsed
}

func getTaskQuota(t *testing.T, id int64) int {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&task).Error)
	return task.Quota
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

// ===========================================================================
// RefundTaskQuota tests
// ===========================================================================

func TestRefundTaskQuota_Wallet(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 1, 1, 1
	const initQuota, preConsumed = 10000, 3000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-test-key", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusFailure, 0, "task failed: upstream error"))

	// User quota should increase by preConsumed
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))

	// Token remain_quota should increase, used_quota should decrease
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	// A refund log should be created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed, log.Quota)
	assert.Equal(t, "test-model", log.ModelName)
	assert.Zero(t, task.Quota)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_Subscription(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 2, 2, 2, 1
	const preConsumed = 2000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-key", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusFailure, 0, "subscription task failed"))

	// Subscription used should decrease by preConsumed
	assert.Equal(t, subUsed-int64(preConsumed), getSubscriptionUsed(t, subID))

	// Token should also be refunded
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	assert.Zero(t, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_ZeroQuota(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 3
	seedUser(t, userID, 5000)

	task := makeTask(userID, 0, 0, 0, BillingSourceWallet, 0)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusFailure, 0, "zero quota task"))

	// No change to user quota
	assert.Equal(t, 5000, getUserQuota(t, userID))

	// No log created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRefundTaskQuota_NoToken(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 4, 4
	const initQuota, preConsumed = 10000, 1500

	seedUser(t, userID, initQuota)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet, 0) // TokenId=0
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusFailure, 0, "no token task failed"))

	// User quota refunded
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	// Log created
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Zero(t, getTaskQuota(t, task.ID))
}

func TestRefundTaskQuota_FundingFailureKeepsAccountingAndPendingMarker(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID, preConsumed = 5, 5, 1200
	seedUser(t, userID, 5000)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, 0, preConsumed, 1)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceSubscription, 9999)
	require.NoError(t, model.DB.Create(task).Error)

	require.Error(t, completeTestTask(t, ctx, task, model.TaskStatusFailure, 0, "subscription missing"))
	assert.Equal(t, 5000, getUserQuota(t, userID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, preConsumed, getTaskQuota(t, task.ID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))
	assert.Equal(t, int64(0), countLogs(t))
}

// ===========================================================================
// RecalculateTaskQuota tests
// ===========================================================================

func TestRecalculate_PositiveDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 10, 10, 10
	const initQuota, preConsumed = 10000, 2000
	const actualQuota = 3000 // under-charged by 1000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-pos", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, actualQuota, "adaptor adjustment"))

	// User quota should decrease by the delta (1000 additional charge)
	assert.Equal(t, initQuota-(actualQuota-preConsumed), getUserQuota(t, userID))

	// Token should also be charged the delta
	assert.Equal(t, tokenRemain-(actualQuota-preConsumed), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Consume (additional charge)
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeConsume, log.Type)
	assert.Equal(t, actualQuota-preConsumed, log.Quota)
}

func TestRecalculate_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 11, 11, 11
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged by 2000
	const tokenRemain = 5000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-recalc-neg", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, actualQuota, "adaptor adjustment"))

	// User quota should increase by abs(delta) = 2000 (refund overpayment)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))

	// Token should be refunded the difference
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota updated
	assert.Equal(t, actualQuota, task.Quota)

	// Log type should be Refund
	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
	assert.Equal(t, preConsumed-actualQuota, log.Quota)
}

func TestRecalculate_ZeroDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 12
	const initQuota, preConsumed = 10000, 3000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, preConsumed, 0, BillingSourceWallet, 0)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, preConsumed, "exact match"))

	// No change to user quota
	assert.Equal(t, initQuota, getUserQuota(t, userID))

	// No log created (delta is zero)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestRecalculate_ActualQuotaZero(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID = 13
	const initQuota = 10000

	seedUser(t, userID, initQuota)

	task := makeTask(userID, 0, 5000, 0, BillingSourceWallet, 0)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, 0, "zero actual"))

	// An explicitly known final zero returns the entire submission charge.
	assert.Equal(t, initQuota+5000, getUserQuota(t, userID))
	assert.Equal(t, int64(1), countLogs(t))
}

func TestRecalculate_Subscription_NegativeDelta(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID, subID = 14, 14, 14, 2
	const preConsumed = 5000
	const actualQuota = 2000 // over-charged by 3000
	const subTotal, subUsed int64 = 100000, 50000
	const tokenRemain = 8000

	seedUser(t, userID, 0)
	seedToken(t, tokenID, userID, "sk-sub-recalc", tokenRemain)
	seedChannel(t, channelID)
	seedSubscription(t, subID, userID, subTotal, subUsed)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceSubscription, subID)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, actualQuota, "subscription over-charge"))

	// Subscription used should decrease by delta (refund 3000)
	assert.Equal(t, subUsed-int64(preConsumed-actualQuota), getSubscriptionUsed(t, subID))

	// Token refunded
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, actualQuota, getTokenUsedQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	assert.Equal(t, actualQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

// ===========================================================================
// CAS + Billing integration tests
// Exercises the production terminal transaction and recovery entry point.
// ===========================================================================

func completeTestTask(t *testing.T, ctx context.Context, task *model.Task, status model.TaskStatus, actualQuota int, reason string) error {
	t.Helper()
	if task.ID == 0 {
		require.NoError(t, model.DB.Create(task).Error)
	}
	fromStatus := task.Status
	task.Status, task.Progress = status, "100%"
	_, err := completeTaskBilling(ctx, task, fromStatus, actualQuota, reason)
	return err
}

func TestCASGuardedRefund_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 20, 20, 20
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-win", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusFailure, 0, "upstream error"))

	// CAS wins: task in DB should now be FAILURE
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, reloaded.Status)
	assert.Zero(t, reloaded.Quota)

	// Refund should have happened
	assert.Equal(t, initQuota+preConsumed, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+preConsumed, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Zero(t, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Zero(t, getChannelUsedQuota(t, channelID))

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}

func TestCASGuardedRefund_Lose(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 6000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-refund-lose", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	// Create task with IN_PROGRESS in DB
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	// Simulate another process already transitioning to FAILURE
	model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("status", model.TaskStatusFailure)

	// Our process still has the old in-memory state (IN_PROGRESS) and tries to transition
	// task.Status is still IN_PROGRESS in the snapshot
	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusFailure, 0, "upstream error"))

	// CAS lost: user quota should NOT change (no double refund)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, preConsumed, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(preConsumed), getChannelUsedQuota(t, channelID))

	// No billing log should be created
	assert.Equal(t, int64(0), countLogs(t))
}

func TestCASGuardedSettle_Win(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 22, 22, 22
	const initQuota, preConsumed = 10000, 5000
	const actualQuota = 3000 // over-charged, should get partial refund
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-cas-settle-win", tokenRemain)
	seedChannel(t, channelID)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.Status = model.TaskStatus(model.TaskStatusInProgress)
	require.NoError(t, model.DB.Create(task).Error)

	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, actualQuota, "test settle"))

	// CAS wins: task should be SUCCESS
	var reloaded model.Task
	require.NoError(t, model.DB.First(&reloaded, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, reloaded.Status)

	// Settlement should refund the over-charge (5000 - 3000 = 2000 back to user)
	assert.Equal(t, initQuota+(preConsumed-actualQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-actualQuota), getTokenRemainQuota(t, tokenID))
	usedQuota, requestCount := getUserUsageAccounting(t, userID)
	assert.Equal(t, actualQuota, usedQuota)
	assert.Equal(t, 1, requestCount)
	assert.Equal(t, int64(actualQuota), getChannelUsedQuota(t, channelID))

	// task.Quota should be updated to actualQuota
	assert.Equal(t, actualQuota, task.Quota)
}

// ===========================================================================
// Mock adaptor for settleTaskBillingOnComplete tests
// ===========================================================================

type mockAdaptor struct {
	adjustReturn int
}

func (m *mockAdaptor) Init(_ *relaycommon.RelayInfo) {}
func (m *mockAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, nil
}
func (m *mockAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) { return nil, nil }
func (m *mockAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return m.adjustReturn
}

// ===========================================================================
// PerCallBilling tests — settleTaskBillingOnComplete
// ===========================================================================

func TestSettle_PerCallBilling_SkipsAdaptorAdjust(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 30, 30, 30
	const initQuota, preConsumed = 10000, 5000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-adaptor", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 2000}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	quota, reason, _, err := taskCompletionQuota(adaptor, task, taskResult)
	require.NoError(t, err)
	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, quota, reason))

	// Per-call: no adjustment despite adaptor returning 2000
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_PerCallBilling_SkipsTotalTokens(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 31, 31, 31
	const initQuota, preConsumed = 10000, 4000
	const tokenRemain = 7000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-percall-tokens", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	task.PrivateData.BillingContext.PerCallBilling = true

	adaptor := &mockAdaptor{adjustReturn: 0}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, TotalTokens: 9999}

	quota, reason, _, err := taskCompletionQuota(adaptor, task, taskResult)
	require.NoError(t, err)
	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, quota, reason))

	// Per-call: no recalculation by tokens
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, preConsumed, task.Quota)
	assert.Equal(t, int64(0), countLogs(t))
}

func TestSettle_NonPerCallBilling_AppliesAdaptorAdjustment(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 32, 32, 32
	const initQuota, preConsumed = 10000, 5000
	const adaptorQuota = 3000
	const tokenRemain = 8000

	seedUser(t, userID, initQuota)
	seedToken(t, tokenID, userID, "sk-nonpercall-adj", tokenRemain)
	seedChannel(t, channelID)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet, 0)
	// PerCallBilling defaults to false

	adaptor := &mockAdaptor{adjustReturn: adaptorQuota}
	taskResult := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess}

	quota, reason, _, err := taskCompletionQuota(adaptor, task, taskResult)
	require.NoError(t, err)
	require.NoError(t, completeTestTask(t, ctx, task, model.TaskStatusSuccess, quota, reason))

	// Non-per-call: adaptor adjustment applies (refund 2000)
	assert.Equal(t, initQuota+(preConsumed-adaptorQuota), getUserQuota(t, userID))
	assert.Equal(t, tokenRemain+(preConsumed-adaptorQuota), getTokenRemainQuota(t, tokenID))
	assert.Equal(t, adaptorQuota, task.Quota)

	log := getLastLog(t)
	require.NotNil(t, log)
	assert.Equal(t, model.LogTypeRefund, log.Type)
}
