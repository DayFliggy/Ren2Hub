package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperUsesNativeCacheWriteFallback(t *testing.T) {
	quotaSetting := operation_setting.GetQuotaSetting()
	savedMultiplier := quotaSetting.PreConsumeMultiplier
	quotaSetting.PreConsumeMultiplier = 1
	t.Cleanup(func() { quotaSetting.PreConsumeMultiplier = savedMultiplier })
	savedModel := ratio_setting.ModelRatio2JSONString()
	savedPrice := ratio_setting.ModelPrice2JSONString()
	savedCompletion := ratio_setting.CompletionRatio2JSONString()
	savedCache := ratio_setting.CreateCacheRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedModel))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrice))
		require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(savedCompletion))
		require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(savedCache))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"cache-fallback-test":1}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateCompletionRatioByJSONString(`{"cache-fallback-test":1}`))
	for _, tc := range []struct {
		name, values string
		ratio        float64
		configured   bool
	}{
		{"unset", `{}`, 1.25, false},
		{"explicit_zero", `{"cache-fallback-test":0}`, 0, true},
		{"explicit_one", `{"cache-fallback-test":1}`, 1, true},
		{"explicit_one_point_five", `{"cache-fallback-test":1.5}`, 1.5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(tc.values))
			ratio, configured := ratio_setting.GetCreateCacheRatio("cache-fallback-test")
			require.Equal(t, tc.configured, configured)
			require.Equal(t, tc.ratio, ratio)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			ctx.Set("channel_ratio", "1")
			info := &relaycommon.RelayInfo{OriginModelName: "cache-fallback-test"}
			price, err := ModelPriceHelper(ctx, info, 100, &types.TokenCountMeta{})
			require.NoError(t, err)
			require.Equal(t, tc.ratio, price.CacheCreationRatio)
			require.Equal(t, tc.ratio, price.CacheCreation5mRatio)
			require.InDelta(t, tc.ratio*1.6, price.CacheCreation1hRatio, 0.000001)
			require.Equal(t, tc.ratio*2, price.CacheCreation12hRatio)
			require.Equal(t, 100, price.QuotaToPreConsume, "input reservation remains independent of cache-write fallback")
			t.Logf("CACHE_HELPER ratio=%g configured=%t reservation=%d short=%g one_hour=%g twelve_hour=%g", ratio, configured, price.QuotaToPreConsume, price.CacheCreation5mRatio, price.CacheCreation1hRatio, price.CacheCreation12hRatio)
		})
	}
}
