package service

import (
	"context"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func realtimeBillingSession(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	truncate(t)
	seedUser(t, 880, 100)
	seedToken(t, 881, 880, "realtime-billing-test", 100)
	seedChannel(t, 882)
	beforeExport := common.DataExportEnabled
	common.DataExportEnabled = false
	t.Cleanup(func() { common.DataExportEnabled = beforeExport })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		RequestId: "realtime-billing-session", UserId: 880, TokenId: 881, TokenKey: "realtime-billing-test",
		RelayFormat: types.RelayFormatOpenAIRealtime, OriginModelName: "realtime-test-model", StartTime: time.Now(),
		PriceData:   hosttypes.PriceData{ModelRatio: 1, ChannelRatio: 1},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 882, UpstreamModelName: "realtime-test-model"},
	}
	info.PriceData.Quote = billingTestQuote(t, "p * 2 + c * 2")
	require.Nil(t, PreConsumeBilling(c, 5, info))
	t.Cleanup(func() { info.Billing.(*BillingSession).stopRecoveryHeartbeat() })
	return c, info
}

func TestRealtimeReservesCumulativeUsageAndSettlesOnce(t *testing.T) {
	c, info := realtimeBillingSession(t)
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 20}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 20}))
	assert.EqualValues(t, 20, info.Billing.GetPreConsumedQuota())
	for i := 0; i < 2; i++ {
		PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 20}, "")
		require.NoError(t, info.BillingSettlementError)
	}
	info.Billing.Refund(c)
	var user model.User
	var token model.Token
	var channel model.Channel
	var logCount int64
	require.NoError(t, model.DB.First(&user, 880).Error)
	require.NoError(t, model.DB.First(&token, 881).Error)
	require.NoError(t, model.DB.First(&channel, 882).Error)
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", info.RequestId).Count(&logCount).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, 20, user.UsedQuota)
	assert.EqualValues(t, 1, user.RequestCount)
	assert.EqualValues(t, 80, token.RemainQuota)
	assert.EqualValues(t, 20, token.UsedQuota)
	assert.EqualValues(t, int64(20), channel.UsedQuota)
	assert.EqualValues(t, int64(1), logCount)
}

func TestRealtimeReservationStopsAtAvailableFunds(t *testing.T) {
	c, info := realtimeBillingSession(t)
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 60}))
	assert.ErrorIs(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 120}), model.ErrBillingRecoveryFundingQuota)
	assert.EqualValues(t, 60, info.Billing.GetPreConsumedQuota())
	var user model.User
	require.NoError(t, model.DB.First(&user, 880).Error)
	assert.EqualValues(t, 40, user.Quota)
	PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 120}, "")
	require.NoError(t, info.BillingSettlementError)
	require.NoError(t, model.DB.First(&user, 880).Error)
	assert.EqualValues(t, -20, user.Quota, "already incurred usage is settled once even when the session cannot reserve further")
}

func TestRealtimePerCallPriceIsReservedAndChargedOncePerSession(t *testing.T) {
	c, info := realtimeBillingSession(t)
	info.PriceData.UsePrice = true
	info.PriceData.Quote = billingTestCallQuote(t)
	info.MarkValidOutput()
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{}))
	assert.EqualValues(t, 20, info.Billing.GetPreConsumedQuota())
	PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{}, "")
	require.NoError(t, info.BillingSettlementError)
	var user model.User
	require.NoError(t, model.DB.First(&user, 880).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, 1, user.RequestCount)
}

func TestRealtimeConsumptionRecoveryFinishesAfterLostProcess(t *testing.T) {
	c, info := realtimeBillingSession(t)
	log, err := model.BuildConsumeLog(c, info.UserId, model.RecordConsumeLogParams{TokenId: info.TokenId, ChannelId: info.ChannelId, ModelName: info.OriginModelName, PromptTokens: 20, Quota: 20})
	require.NoError(t, err)
	require.NoError(t, model.PrepareBillingRecoveryConsumption(info.RequestId, model.BillingConsumption{Log: *log, CountUsage: true, LogEnabled: true}))
	info.Billing.(*BillingSession).stopRecoveryHeartbeat()
	summary, err := RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+1, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Settled)
	var user model.User
	require.NoError(t, model.DB.First(&user, 880).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, 20, user.UsedQuota)
	assert.EqualValues(t, 1, user.RequestCount)
	summary, err = RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+1, 100)
	require.NoError(t, err)
	assert.Zero(t, summary.Scanned)
}

func TestRealtimeRecoverySettlesTheLastConfirmedRoundAfterSessionCrash(t *testing.T) {
	c, info := realtimeBillingSession(t)
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 20}))
	assert.ErrorIs(t, model.MarkBillingRecoveryRefundPending(info.RequestId, nil), model.ErrBillingRecoveryStateConflict)
	info.Billing.(*BillingSession).stopRecoveryHeartbeat()
	summary, err := RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+1, 100)
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Settled)
	assert.Zero(t, summary.Refunded)
	var user model.User
	var logs []model.Log
	require.NoError(t, model.DB.First(&user, 880).Error)
	require.NoError(t, model.LOG_DB.Where("request_id = ?", info.RequestId).Find(&logs).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, 20, user.UsedQuota)
	assert.EqualValues(t, 1, user.RequestCount)
	require.Len(t, logs, 1)
	assert.EqualValues(t, 20, logs[0].Quota)
}

func TestRealtimeUsageRejectsNegativeAndOverflowCounts(t *testing.T) {
	for _, usage := range []*dto.RealtimeUsage{
		{InputTokens: -1},
		{InputTokenDetails: dto.InputTokenDetails{AudioTokens: -1}},
		{InputTokens: math.MaxInt32, OutputTokens: 1},
		{TotalTokens: 10},
		{InputTokens: 2, OutputTokens: 3, TotalTokens: 6},
		{InputTokens: 2, InputTokenDetails: dto.InputTokenDetails{TextTokens: 3}},
		{OutputTokens: 2, OutputTokenDetails: dto.OutputTokenDetails{AudioTokens: 3}},
		{InputTokens: 2, InputTokenDetails: dto.InputTokenDetails{ImageTokens: -1}},
		{OutputTokens: 2, OutputTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 3}},
	} {
		_, err := NormalizeRealtimeUsage(usage)
		assert.Error(t, err)
	}
	total := &dto.RealtimeUsage{InputTokens: math.MaxInt32}
	_, err := AddRealtimeUsage(total, &dto.RealtimeUsage{InputTokens: 1})
	assert.Error(t, err)
	assert.EqualValues(t, math.MaxInt32, total.InputTokens)
}
