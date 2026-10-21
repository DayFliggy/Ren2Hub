package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const realtimeTestModel = "realtime-e11-model"

// newRealtimeBillingFixture 构造与 controller.Relay 相同的起点：Realtime 估算 0 token，
// PrepareRequestBilling 以 0 预扣建立会话。
func newRealtimeBillingFixture(t *testing.T, userID, wallet int) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	truncate(t)
	// 与 controller/token_test.go 相同：初始化生产环境的列名引用，供按 Key 查令牌使用。
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	previousRatios := ratio_setting.ModelRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"`+realtimeTestModel+`":1}`))
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios)) })

	seedUser(t, userID, wallet)
	key := "realtime-" + time.Now().Format("150405.000000")
	seedToken(t, userID, userID, key, 1_000_000)
	seedChannel(t, userID)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{
		UserId: userID, TokenId: userID, TokenKey: key,
		OriginModelName: realtimeTestModel, RelayFormat: types.RelayFormatOpenAIRealtime,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: userID, UpstreamModelName: realtimeTestModel},
		StartTime:   time.Now(),
		PriceData: hosttypes.PriceData{
			ModelRatio:       1,
			ChannelRatioInfo: hosttypes.ChannelRatioInfo{ChannelRatio: 1},
		},
	}
	require.Nil(t, PreConsumeBilling(ctx, 0, info))
	return ctx, info
}

// realtimeRound 与 relay/channel/openai/relay_realtime.go preConsumeUsage 相同：
// 先把本轮用量累加进会话总量，再调用 PreWssConsumeQuota。
func realtimeRound(ctx *gin.Context, info *relaycommon.RelayInfo, total *dto.RealtimeUsage, inputTokens int) error {
	total.TotalTokens += inputTokens
	total.InputTokens += inputTokens
	total.InputTokenDetails.TextTokens += inputTokens
	round := &dto.RealtimeUsage{TotalTokens: inputTokens, InputTokens: inputTokens}
	round.InputTokenDetails.TextTokens = inputTokens
	return PreWssConsumeQuota(ctx, info, round)
}

func requireRealtimeBalances(t *testing.T, userID, wantWallet, wantTokenRemain int) {
	t.Helper()
	wallet, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	token, err := model.GetTokenById(userID)
	require.NoError(t, err)
	assert.Equal(t, wantWallet, wallet, "wallet balance")
	assert.Equal(t, wantTokenRemain, token.RemainQuota, "token remain quota")
}

func TestRealtimeTwoRoundsChargeUsageOnce(t *testing.T) {
	const userID, wallet = 1101, 100_000
	ctx, info := newRealtimeBillingFixture(t, userID, wallet)
	total := &dto.RealtimeUsage{}

	require.NoError(t, realtimeRound(ctx, info, total, 1000))
	require.NoError(t, realtimeRound(ctx, info, total, 2000))
	PostWssConsumeQuota(ctx, info, realtimeTestModel, total, nil)

	requireRealtimeBalances(t, userID, wallet-3000, 1_000_000-3000)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ?", userID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, 3000, logs[0].Quota)
}

func TestRealtimeInsufficientSecondRoundClosesWithoutDebt(t *testing.T) {
	const userID, wallet = 1102, 2000
	ctx, info := newRealtimeBillingFixture(t, userID, wallet)
	total := &dto.RealtimeUsage{}

	require.NoError(t, realtimeRound(ctx, info, total, 1000))
	err := realtimeRound(ctx, info, total, 2000)
	require.Error(t, err, "the adaptor closes the session when a round cannot be reserved")
	var apiErr *types.NewAPIError
	if assert.ErrorAs(t, err, &apiErr) {
		assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	}
	// 适配器在会话结束时对未清零的本轮用量再走一次 preConsumeUsage（忽略错误），然后结算。
	_ = realtimeRound(ctx, info, total, 2000)
	PostWssConsumeQuota(ctx, info, realtimeTestModel, total, nil)

	wantCharged := 1000
	requireRealtimeBalances(t, userID, wallet-wantCharged, 1_000_000-wantCharged)
	balance, err := model.GetUserQuota(userID, true)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, balance, 0, "realtime billing must never leave the wallet negative")
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("user_id = ?", userID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, wantCharged, logs[0].Quota)
}
