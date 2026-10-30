package service

import (
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
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The violation fee is a penalty: it uses the configured channel ratio even
// when the request carried a usage discount, and it still counts as usage.
func TestViolationFeeIgnoresUsageDiscount(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	previousLogDB, previousRedis, previousBatch, previousLogConsume := model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = db, false, false, true
	t.Cleanup(func() {
		model.LOG_DB, common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousLogDB, previousRedis, previousBatch, previousLogConsume
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Log{}))
	user := model.User{Username: "fee-user", Quota: 1_000_000}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "fee-token-key", Name: "fee", RemainQuota: 1_000_000}
	require.NoError(t, db.Create(&token).Error)
	settings := model_setting.GetGrokSettings()
	original := *settings
	settings.ViolationDeductionEnabled, settings.ViolationDeductionAmount = true, 0.1
	t.Cleanup(func() { *settings = original })

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	decision := hosttypes.NeutralUsageDiscount(hosttypes.UsageDiscountOK, "")
	decision.Active, decision.Factor = true, decimal.RequireFromString("0.95")
	info := &relaycommon.RelayInfo{
		UserId: user.Id, TokenId: token.Id, TokenKey: token.Key, OriginModelName: "grok-test", StartTime: time.Now(),
		ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 3},
		PriceData:   hosttypes.PriceData{ChannelRatioInfo: hosttypes.ChannelRatioInfo{ChannelRatio: 1.9, BaseChannelRatio: 2, UsageDiscount: decision}},
	}
	apiErr := types.NewError(errors.New("upstream: "+CSAMViolationMarker), types.ErrorCodeBadResponse)

	require.True(t, ChargeViolationFeeIfNeeded(ctx, info, apiErr))
	const fee = 100_000 // 0.1 USD × 500000 × base ratio 2
	var charged model.User
	require.NoError(t, db.First(&charged, user.Id).Error)
	assert.Equal(t, 1_000_000-fee, charged.Quota)
	var hourly model.UsageDiscountHourly
	require.NoError(t, db.First(&hourly, "user_id = ?", user.Id).Error)
	assert.Equal(t, int64(fee), hourly.Quota)
	var log model.Log
	require.NoError(t, db.Where("user_id = ?", user.Id).First(&log).Error)
	assert.Contains(t, log.Other, `"channel_ratio":"2"`)
	assert.NotContains(t, log.Other, "usage_discount")
}
