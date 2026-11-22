package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCacheCreationOnlyUsageChargesWithoutOrdinaryTokens(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, OriginModelName: "cache-only", StartTime: time.Now(), PriceData: hosttypes.PriceData{ModelRatio: 1, CacheCreationRatio: 2, ChannelRatioInfo: hosttypes.ChannelRatioInfo{ChannelRatio: 1}}}
	summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{PromptTokensDetails: dto.InputTokenDetails{CacheCreationInputTokens: 7}})
	require.Equal(t, 14, summary.Quota)
	require.True(t, summary.hasBillableUsage())
	empty := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.Zero(t, empty.Quota)
}

func TestCacheWriteLogAggregateIncludes12hWithoutDoubleCounting(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		total, short, hour, halfday, want int
	}{
		{"split", 0, 10, 20, 40, 70}, {"12h only", 0, 0, 0, 40, 40},
		{"aggregate includes split", 100, 10, 20, 40, 100}, {"equal", 70, 10, 20, 40, 70},
		{"legacy", 50, 0, 0, 0, 50}, {"empty", 0, 0, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, cacheWriteTokensTotal(textQuotaSummary{CacheCreationTokens: tc.total, CacheCreationTokens5m: tc.short, CacheCreationTokens1h: tc.hour, CacheCreationTokens12h: tc.halfday}))
		})
	}
}
