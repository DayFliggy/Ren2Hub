package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func usageDiscountPriceContext(ratio string) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("channel_ratio", ratio)
	return c
}

func usageDiscountPriceInfo(modelName string, factor string) *relaycommon.RelayInfo {
	decision := hosttypes.NeutralUsageDiscount(hosttypes.UsageDiscountOK, "")
	decision.Active, decision.Factor = true, decimal.RequireFromString(factor)
	return &relaycommon.RelayInfo{OriginModelName: modelName, UsageDiscount: decision}
}

// usageDiscountPriceModels registers a per-token and a per-call test model.
func usageDiscountPriceModels(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ud-ratio-model":1.5}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"ud-price-model":0.01}`))
}

// A request without a decision resolves one from the saved configuration;
// this covers the path from storage to the effective ratio.
func TestUsageDiscountPriceResolvesSavedConfig(t *testing.T) {
	usageDiscountPriceModels(t)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.UsageDiscountHourly{}, &model.UsageDiscountConfigVersion{}))
	previousDB := model.DB
	model.DB = db
	service.ResetUsageDiscountCaches()
	t.Cleanup(func() {
		model.DB = previousDB
		service.ResetUsageDiscountCaches()
		require.NoError(t, sqlDB.Close())
	})
	config := model.DefaultUsageDiscountConfig()
	config.FixedRate = "0.02"
	data, err := common.Marshal(config)
	require.NoError(t, err)
	_, err = model.SaveUsageDiscountConfig(data, 0, 1, time.Now())
	require.NoError(t, err)
	service.ResetUsageDiscountCaches() // read the saved row, not the snapshot the save left

	info := &relaycommon.RelayInfo{UserId: 7, OriginModelName: "ud-ratio-model"}
	priceData, err := ModelPriceHelper(usageDiscountPriceContext("2"), info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.NotNil(t, info.UsageDiscount)
	assert.True(t, info.UsageDiscount.Active)
	assert.Equal(t, "0.98", info.UsageDiscount.Factor.String())
	assert.InDelta(t, 1.96, priceData.ChannelRatioInfo.ChannelRatio, 1e-12)
	assert.Equal(t, 2.0, priceData.ChannelRatioInfo.BaseRatio())
}

// A 5% discount on channel ratio 2 must price exactly like an undiscounted
// channel ratio of 1.9, for both the per-token and the per-call entries.
func TestUsageDiscountFactorAppliesOnceInEveryPricingEntry(t *testing.T) {
	usageDiscountPriceModels(t)

	discounted := usageDiscountPriceInfo("ud-ratio-model", "0.95")
	priceData, err := ModelPriceHelper(usageDiscountPriceContext("2"), discounted, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	reference, err := ModelPriceHelper(usageDiscountPriceContext("1.9"), usageDiscountPriceInfo("ud-ratio-model", "1"), 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, reference.QuotaToPreConsume, priceData.QuotaToPreConsume)
	assert.InDelta(t, 1.9, priceData.ChannelRatioInfo.ChannelRatio, 1e-12)
	assert.Equal(t, 2.0, priceData.ChannelRatioInfo.BaseRatio())

	perCall, err := ModelPriceHelperPerCall(usageDiscountPriceContext("2"), usageDiscountPriceInfo("ud-price-model", "0.95"))
	require.NoError(t, err)
	perCallReference, err := ModelPriceHelperPerCall(usageDiscountPriceContext("1.9"), usageDiscountPriceInfo("ud-price-model", "1"))
	require.NoError(t, err)
	assert.Equal(t, perCallReference.Quota, perCall.Quota)
	assert.Equal(t, 9500, perCall.Quota)

	// A retry on another channel reuses the request's decision instead of
	// resolving again, so the factor never changes or stacks mid-request.
	decision := discounted.UsageDiscount
	retry, err := ModelPriceHelper(usageDiscountPriceContext("4"), discounted, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Same(t, decision, discounted.UsageDiscount)
	assert.InDelta(t, 3.8, retry.ChannelRatioInfo.ChannelRatio, 1e-12)
}
