package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useTopUpBonusUsageTable adds the hourly table to the shared task-billing
// database, so a missing write is not hidden by a failing one, and isolates
// the cached business day.
func useTopUpBonusUsageTable(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.TopUpBonusUsageHourly{}, &model.TopUpBonusConfigVersion{}))
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.TopUpBonusUsageHourly{}).Error)
	previousDay := currentTopUpBonusDay.Load()
	ResetTopUpBonusCaches()
	t.Cleanup(func() {
		require.NoError(t, model.DB.Where("1 = 1").Delete(&model.TopUpBonusUsageHourly{}).Error)
		ResetTopUpBonusCaches()
		currentTopUpBonusDay.Store(previousDay)
	})
}

func topUpBonusUsageRows(t *testing.T) []model.TopUpBonusUsageHourly {
	t.Helper()
	var rows []model.TopUpBonusUsageHourly
	require.NoError(t, model.DB.Order("user_id, hour_utc").Find(&rows).Error)
	return rows
}

func taskSubmitInfo(userID, channelID, tokenID, quota int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          userID,
		TokenId:         tokenID,
		OriginModelName: "test-model",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: channelID},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{Action: "GENERATE"},
		PriceData:       hosttypes.PriceData{ModelPrice: 0.02, Quota: quota, ChannelRatioInfo: hosttypes.ChannelRatioInfo{ChannelRatio: 1}},
	}
}

// Task billing (tasks and midjourneys) stays out of the top-up bonus window:
// after an earlier synchronous usage S, submission, settlement differences in
// both directions and refunds move used_quota, the wallet and the token as
// before while the hourly table still holds exactly S.
func TestTopUpBonusWindowIgnoresAsyncTaskBilling(t *testing.T) {
	truncate(t)
	useTopUpBonusUsageTable(t)
	ctx := context.Background()
	const userID, tokenID, channelID = 70, 70, 70
	const wallet, tokenRemain = 100_000, 100_000
	seedUser(t, userID, wallet)
	seedToken(t, tokenID, userID, "sk-bonus-async", tokenRemain)
	seedChannel(t, channelID)
	hour := time.Now().Unix() / 3600 * 3600
	const syncUsage = 1234
	synced := model.TopUpBonusUsageHourly{UserID: userID, HourUTC: hour - 3600, Quota: syncUsage}
	require.NoError(t, model.DB.Create(&synced).Error)
	requireOnlySyncUsage := func(step string) {
		t.Helper()
		assert.Equal(t, []model.TopUpBonusUsageHourly{synced}, topUpBonusUsageRows(t), step)
	}

	task := makeTask(userID, channelID, 2000, tokenID, BillingSourceWallet)
	callLogTaskConsumption(t, taskSubmitInfo(userID, channelID, tokenID, 2000), task)
	used, requests := getUserUsageAccounting(t, userID)
	assert.Equal(t, 2000, used, "submission adds used_quota")
	assert.Equal(t, 1, requests, "submission counts one request")
	requireOnlySyncUsage("submission")

	RecalculateTaskQuota(ctx, task, 3000, common.NewMessage("adaptor adjustment"))
	used, requests = getUserUsageAccounting(t, userID)
	assert.Equal(t, 3000, used, "an upward difference adds used_quota")
	assert.Equal(t, wallet-1000, getUserQuota(t, userID), "the wallet pays the upward difference")
	assert.Equal(t, tokenRemain-1000, getTokenRemainQuota(t, tokenID))
	requireOnlySyncUsage("upward difference")

	RecalculateTaskQuota(ctx, task, 2500, common.NewMessage("adaptor adjustment"))
	used, requests = getUserUsageAccounting(t, userID)
	assert.Equal(t, 2500, used, "a downward difference takes used_quota back")
	assert.Equal(t, 1, requests)
	assert.Equal(t, wallet-500, getUserQuota(t, userID))
	requireOnlySyncUsage("downward difference")

	require.NoError(t, model.DB.Create(task).Error)
	assert.True(t, RefundTaskQuota(ctx, task, "task failed"))
	used, requests = getUserUsageAccounting(t, userID)
	assert.Equal(t, 0, used, "the refund takes the settled quota back out of used_quota")
	assert.Equal(t, 1, requests)
	assert.Equal(t, wallet+2000, getUserQuota(t, userID), "the refund returns the settled 2500")
	requireOnlySyncUsage("task refund")

	seedChargedAccounting(t, userID, channelID, 0, 3000, 2)
	mj := &model.Midjourney{UserId: userID, MjId: "mj-bonus-async", Action: "IMAGINE", ChannelId: channelID, Quota: 3000, Progress: "0%"}
	require.NoError(t, mj.Insert())
	assert.True(t, RefundMidjourneyQuota(ctx, mj, "midjourney failed"))
	used, requests = getUserUsageAccounting(t, userID)
	assert.Zero(t, used)
	assert.Equal(t, 2, requests)
	requireOnlySyncUsage("midjourney refund")
}

// A task submitted before midnight and refunded after it leaves the next
// day's personal windows and the site total at the earlier synchronous usage
// S: the submission is not yesterday's usage and the refund is not today's.
func TestTaskRefundedAcrossMidnightLeavesNextDayWindowsEmpty(t *testing.T) {
	truncate(t)
	useTopUpBonusUsageTable(t)
	const userID, tokenID, channelID = 71, 71, 71
	const quota, syncUsage = 200_000, 50
	seedUser(t, userID, 1_000_000)
	seedToken(t, tokenID, userID, "sk-bonus-midnight", 1_000_000)
	seedChannel(t, channelID)
	settings := topUpBonusTestSettings(t, 1, nil)
	now := time.Now()
	day, err := newTopUpBonusDay(settings.Location, now)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.TopUpBonusUsageHourly{UserID: userID, HourUTC: day.cutoff - 7200, Quota: syncUsage}).Error)

	task := makeTask(userID, channelID, quota, tokenID, BillingSourceWallet)
	callLogTaskConsumption(t, taskSubmitInfo(userID, channelID, tokenID, quota), task)
	// Whatever the submission wrote belongs to the hour before midnight.
	require.NoError(t, model.DB.Model(&model.TopUpBonusUsageHourly{}).Where("user_id = ? AND hour_utc >= ?", userID, day.cutoff).
		Update("hour_utc", day.cutoff-3600).Error)
	require.NoError(t, model.DB.Create(task).Error)
	assert.True(t, RefundTaskQuota(context.Background(), task, "task failed after midnight"))

	result := mustResolve(t, settings, userID, 10_000, now)
	assert.Equal(t, windows(syncUsage, syncUsage, syncUsage), result.Personal)
	sums, err := model.SumTopUpBonusByUser(day.monthStart, day.cutoff)
	require.NoError(t, err)
	assert.Equal(t, []model.TopUpBonusUserQuota{{UserID: userID, Quota: syncUsage}}, sums, "the site total holds only S")
	used, _ := getUserUsageAccounting(t, userID)
	assert.Zero(t, used)
}

// A violation fee is synchronous billing: it enters the bonus window once,
// with the amount it adds to used_quota.
func TestViolationFeeEntersTheTopUpBonusWindow(t *testing.T) {
	truncate(t)
	useTopUpBonusUsageTable(t)
	settings := model_setting.GetGrokSettings()
	previous := *settings
	settings.ViolationDeductionEnabled, settings.ViolationDeductionAmount = true, 0.05
	t.Cleanup(func() { *settings = previous })
	const userID, tokenID, channelID = 72, 72, 72
	seedUser(t, userID, 1_000_000)
	seedToken(t, tokenID, userID, "sk-bonus-violation", 1_000_000)
	seedChannel(t, channelID)
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{UserId: userID, TokenId: tokenID, TokenKey: "sk-bonus-violation", OriginModelName: "grok-test",
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: channelID}, StartTime: time.Now(),
		PriceData: hosttypes.PriceData{ChannelRatioInfo: hosttypes.ChannelRatioInfo{ChannelRatio: 1}}}
	apiErr := types.NewErrorWithStatusCode(errors.New("blocked"), types.ErrorCodeViolationFeeGrokCSAM, http.StatusBadRequest)

	require.True(t, ChargeViolationFeeIfNeeded(ctx, info, apiErr))

	const fee = 25_000 // 0.05 x 500000 x channel ratio 1
	used, requests := getUserUsageAccounting(t, userID)
	assert.Equal(t, fee, used)
	assert.Equal(t, 1, requests)
	hour := time.Now().Unix() / 3600 * 3600
	assert.Equal(t, []model.TopUpBonusUsageHourly{{UserID: userID, HourUTC: hour, Quota: fee}}, topUpBonusUsageRows(t))
}
