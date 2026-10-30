package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// A 5% discount on channel ratio 2 must price exactly like an undiscounted
// channel ratio of 1.9, for both the per-token and the per-call entries.
func TestUsageDiscountFactorAppliesOnceInEveryPricingEntry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	savedRatios, savedPrices := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"ud-ratio-model":1.5}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"ud-price-model":0.01}`))

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
