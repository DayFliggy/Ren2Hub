package service

import (
	"context"
	"fmt"
	"math"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func reservedRealtimeBillingSession(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
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
		PriceData:   hosttypes.PriceData{Quote: &billing.Quote{ChannelRatio: "1"}},
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 882, UpstreamModelName: "realtime-test-model"},
	}
	info.PriceData.Quote = billingTestQuote(t, "p * 2 + c * 2")
	info.BillingRequestInput = &billingexpr.RequestInput{}
	info.PriceData.QuotaToPreConsume = 5
	billingTestEstimate(t, info)
	require.Nil(t, PreConsumeBilling(c, 5, info))
	t.Cleanup(func() { info.Billing.(*BillingSession).stopRecoveryHeartbeat() })
	return c, info
}

func realtimeBillingSession(t *testing.T) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	c, info := reservedRealtimeBillingSession(t)
	require.Nil(t, PrepareBillingForSelectedModel(c, info))
	return c, info
}

func TestRealtimeReservesCumulativeUsageAndSettlesOnce(t *testing.T) {
	c, info := realtimeBillingSession(t)
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 20}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 20}))
	assert.EqualValues(t, 20, info.Billing.GetPreConsumedQuota())
	for i := 0; i < 2; i++ {
		PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 20, Observation: confirmedRealtimeObservation()}, "")
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
	PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 120, Observation: confirmedRealtimeObservation()}, "")
	require.NoError(t, info.BillingSettlementError)
	require.NoError(t, model.DB.First(&user, 880).Error)
	assert.EqualValues(t, -20, user.Quota, "already incurred usage is settled once even when the session cannot reserve further")
}

func TestRealtimePerCallPriceIsReservedAndChargedOncePerSession(t *testing.T) {
	c, info := reservedRealtimeBillingSession(t)
	info.PriceData.UsePrice = true
	info.PriceData.Quote = billingTestCallQuote(t)
	info.PriceData.QuotaToPreConsume = 20
	billingTestEstimate(t, info)
	require.Nil(t, PrepareBillingForSelectedModel(c, info))
	info.MarkValidOutput()
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{}))
	assert.EqualValues(t, 20, info.Billing.GetPreConsumedQuota())
	PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{Observation: confirmedRealtimeObservation()}, "")
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
	usage := billing.Usage{InputTokens: 20, Source: "provider", Complete: true, Terminal: "completed"}
	usage, charge, err := model.EvaluateBilling(info.PriceData.Quote, usage, billingexpr.RequestInput{}, info.RequestId, info.BillingAttemptID, "settlement")
	require.NoError(t, err)
	require.NoError(t, model.PrepareBillingRecoveryConsumption(info.RequestId, model.BillingConsumption{AttemptID: info.BillingAttemptID, Log: *log, Usage: &usage, Charge: charge, CountUsage: true, LogEnabled: true}))
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

func TestRealtimeRecoveryRetainsLastUsageUntilSessionOutcomeConfirmed(t *testing.T) {
	c, info := realtimeBillingSession(t)
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 10}))
	require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 20}))
	assert.ErrorIs(t, model.MarkBillingRecoveryRefundPending(info.RequestId, nil), model.ErrBillingRecoveryStateConflict)
	info.Billing.(*BillingSession).stopRecoveryHeartbeat()
	summary, err := RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+1, 100)
	require.NoError(t, err)
	assert.Zero(t, summary.Settled)
	assert.Zero(t, summary.Refunded)
	var user model.User
	var logs []model.Log
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&user, 880).Error)
	require.NoError(t, model.LOG_DB.Where("request_id = ?", info.RequestId).Find(&logs).Error)
	require.NoError(t, model.DB.Where("request_id = ?", info.RequestId).First(&recovery).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, 20, user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	assert.Empty(t, logs)
	assert.Equal(t, model.BillingRecoveryStateUsageCollecting, recovery.ConsumptionState)
	assert.NotEmpty(t, recovery.ConsumptionFingerprint)
	var savedUsage billing.Usage
	require.NoError(t, common.UnmarshalJsonStr(string(recovery.UsageSnapshot), &savedUsage))
	assert.EqualValues(t, 20, savedUsage.InputTokens, "the last known cumulative usage remains available for reconciliation")

	PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 20, Observation: confirmedRealtimeObservation()}, "confirmed final usage")
	require.NoError(t, info.BillingSettlementError)
	require.NoError(t, model.DB.First(&user, 880).Error)
	require.NoError(t, model.LOG_DB.Where("request_id = ?", info.RequestId).Find(&logs).Error)
	assert.Zero(t, user.ReservedQuota)
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

func TestRealtimeProviderZeroCannotBeReplacedByPositiveDetails(t *testing.T) {
	for _, body := range []string{
		`{"input_tokens":0,"output_tokens":0,"input_token_details":{"text_tokens":10}}`,
		`{"input_tokens":0,"output_tokens":0,"input_token_details":{"audio_tokens":10}}`,
		`{"input_tokens":0,"output_tokens":0,"input_token_details":{"image_tokens":10}}`,
		`{"input_tokens":0,"output_tokens":0,"output_token_details":{"text_tokens":10}}`,
		`{"input_tokens":0,"output_tokens":0,"output_token_details":{"audio_tokens":10}}`,
		`{"input_tokens":0,"output_tokens":0,"output_token_details":{"image_tokens":10}}`,
	} {
		var usage dto.RealtimeUsage
		require.NoError(t, common.UnmarshalJsonStr(body, &usage))
		require.True(t, usage.Observation.Complete)
		_, err := NormalizeRealtimeUsage(&usage)
		require.ErrorContains(t, err, "details exceed directional totals", body)
		require.Zero(t, usage.InputTokens)
		require.Zero(t, usage.OutputTokens)
	}
	var zero dto.RealtimeUsage
	require.NoError(t, common.UnmarshalJsonStr(`{"input_tokens":0,"output_tokens":0}`, &zero))
	actual, err := NormalizeRealtimeUsage(&zero)
	require.NoError(t, err)
	require.Zero(t, actual.TotalTokens)
	estimate, err := NormalizeRealtimeUsage(&dto.RealtimeUsage{InputTokenDetails: dto.InputTokenDetails{AudioTokens: 10}})
	require.NoError(t, err)
	require.Equal(t, 10, estimate.InputTokens, "local estimates may derive missing totals")
}

func confirmedRealtimeObservation() *dto.UsageObservation {
	return &dto.UsageObservation{Source: "provider", Complete: true, Terminal: "completed"}
}

func TestRealtimeUnknownOrEstimatedUsageNeverSettles(t *testing.T) {
	for _, observation := range []*dto.UsageObservation{nil,
		{Source: "estimated", Estimated: true, Complete: true, Terminal: "completed"},
		{Source: "provider_and_estimated", Estimated: true, Terminal: "unknown"},
		{Source: "provider", Complete: false, Terminal: "unknown"}} {
		t.Run(fmt.Sprintf("%v", observation), func(t *testing.T) {
			c, info := realtimeBillingSession(t)
			require.NoError(t, PreWssConsumeQuota(c, info, &dto.RealtimeUsage{InputTokens: 20}))
			PostWssConsumeQuota(c, info, info.OriginModelName, &dto.RealtimeUsage{InputTokens: 20, Observation: observation}, "")
			require.Error(t, info.BillingSettlementError)
			info.Billing.Refund(c)
			summary, err := RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+1, 100)
			require.NoError(t, err)
			require.Zero(t, summary.Settled)
			require.Zero(t, summary.Refunded)
			var user model.User
			require.NoError(t, model.DB.First(&user, info.UserId).Error)
			require.Zero(t, user.UsedQuota)
			require.EqualValues(t, 20, user.ReservedQuota)
		})
	}
}

func TestRealtimeAccumulatesModalitiesAndCachePresence(t *testing.T) {
	ptr := func(n int) *int { return &n }
	first := &dto.RealtimeUsage{InputTokens: 40, OutputTokens: 20,
		InputTokenDetails: dto.InputTokenDetails{TextTokens: 10, AudioTokens: 20, ImageTokens: 10, CachedTokens: 8, CacheWriteTokens: 3,
			CachedTokensDetails: &dto.CachedTokenDetails{TextTokens: ptr(1), AudioTokens: ptr(5), ImageTokens: ptr(2)}},
		OutputTokenDetails: dto.OutputTokenDetails{TextTokens: 5, AudioTokens: 10, ImageTokens: 5, ReasoningTokens: 2}}
	total, err := AddRealtimeUsage(first, first)
	require.NoError(t, err)
	require.Equal(t, 20, total.InputTokenDetails.ImageTokens)
	require.Equal(t, 40, total.InputTokenDetails.AudioTokens)
	require.Equal(t, 6, total.InputTokenDetails.CacheCreationTokensTotal())
	require.Equal(t, 4, total.OutputTokenDetails.ReasoningTokens)
	require.Equal(t, 10, total.OutputTokenDetails.ImageTokens)
	require.Equal(t, 10, *total.InputTokenDetails.CachedTokensDetails.AudioTokens)
	require.Equal(t, 4, *total.InputTokenDetails.CachedTokensDetails.ImageTokens)
	require.Equal(t, 5, *first.InputTokenDetails.CachedTokensDetails.AudioTokens)
	missing := &dto.RealtimeUsage{InputTokens: 10, InputTokenDetails: dto.InputTokenDetails{CachedTokens: 2}}
	total, err = AddRealtimeUsage(total, missing)
	require.NoError(t, err)
	require.Nil(t, total.InputTokenDetails.CachedTokensDetails.AudioTokens)
}
