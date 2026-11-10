package service

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestUpstreamUsageAppliesChannelRatioOnce(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{OriginModelName: "upstream-usage-test", PriceData: hosttypes.PriceData{
		ModelRatio: 2, CompletionRatio: 3, CacheRatio: 0.25,
		ChannelRatioInfo: hosttypes.ChannelRatioInfo{ChannelRatio: 0.8},
	}}
	info.SetEstimatePromptTokens(999)
	usage := &dto.Usage{PromptTokens: 100, CompletionTokens: 30, TotalTokens: 130,
		PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 40}}
	summary := calculateTextQuotaSummary(ctx, info, usage)
	assert.Equal(t, 256, summary.Quota, "(60 + 40*0.25 + 30*3) * 2 * 0.8")
	assert.Equal(t, 40, summary.CacheTokens)
	usage = &dto.Usage{PromptTokens: 120, TotalTokens: 120}
	summary = calculateTextQuotaSummary(ctx, info, usage)
	assert.Equal(t, 192, summary.Quota, "reported embeddings input replaces the 999-token estimate")
	assert.Zero(t, summary.CompletionTokens)
}
