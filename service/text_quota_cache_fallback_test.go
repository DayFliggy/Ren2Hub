package service

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCacheWriteFallbackUsesNativeAmounts(t *testing.T) {
	saved := ratio_setting.CreateCacheRatio2JSONString()
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(saved)) })
	for _, tc := range []struct {
		name, values string
		want         [3]int
	}{
		{"unset", `{}`, [3]int{2025, 3150, 3900}},
		{"explicit_zero", `{"cache-fallback-test":0}`, [3]int{150, 150, 150}},
		{"explicit_one", `{"cache-fallback-test":1}`, [3]int{1650, 2550, 3150}},
		{"explicit_one_point_five", `{"cache-fallback-test":1.5}`, [3]int{2400, 3750, 4650}},
	} {
		for i, ttl := range []string{"short_openai", "one_hour_claude", "twelve_hour_openai"} {
			t.Run(tc.name+"/"+ttl, func(t *testing.T) {
				require.NoError(t, ratio_setting.UpdateCreateCacheRatioByJSONString(tc.values))
				ratio, _ := ratio_setting.GetCreateCacheRatio("cache-fallback-test")
				price := hosttypes.PriceData{ModelRatio: 1, CompletionRatio: 1, CacheRatio: 1,
					CacheCreationRatio: ratio, CacheCreation5mRatio: ratio,
					CacheCreation1hRatio: ratio * 1.6, CacheCreation12hRatio: ratio * 2}
				price.ChannelRatioInfo = hosttypes.ChannelRatioInfo{ChannelRatio: 1.5}
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{OriginModelName: "cache-fallback-test", PriceData: price, RelayFormat: types.RelayFormatOpenAI, StartTime: time.Now()}
				usage := &dto.Usage{PromptTokens: 1100, TotalTokens: 1100, UsageSemantic: "openai", PromptTokensDetails: dto.InputTokenDetails{CacheWriteTokens: 1000}}
				if i == 1 {
					usage = &dto.Usage{PromptTokens: 100, TotalTokens: 100, UsageSemantic: "anthropic", ClaudeCacheCreation1hTokens: 1000, PromptTokensDetails: dto.InputTokenDetails{CachedCreationTokens: 1000}}
				} else if i == 2 {
					usage.PromptTokensDetails.CacheWriteTokensDetails = &dto.CacheWriteTokenDetails{TTL12hTokens: 1000}
				}
				summary := calculateTextQuotaSummary(ctx, info, usage)
				require.Equal(t, tc.want[i], summary.Quota)
				t.Logf("CACHE_AMOUNT ratio=%g ttl=%s quota=%d base_input=100 cache_write=1000 route_multiplier=1.5", ratio, ttl, summary.Quota)
			})
		}
	}
}
