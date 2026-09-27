package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

func quotedTextInfo(t *testing.T, prices []billing.UnitPrice) *relaycommon.RelayInfo {
	t.Helper()
	q, err := billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: prices}, "1", nil, time.Now())
	require.NoError(t, err)
	return &relaycommon.RelayInfo{OriginModelName: "test", StartTime: time.Now(), PriceData: hosttypes.PriceData{Quote: q}}
}

func TestDeliveredPerCallStreamChargesOnceWithoutTokenUsage(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.001"}})
	info.PriceData.UsePrice = true
	before := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.NoError(t, before.PricingError)
	assert.Zero(t, before.Quota)
	info.MarkValidOutput()
	after := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.NoError(t, after.PricingError)
	assert.Equal(t, 500, after.Quota)
	assert.Equal(t, common.Quota(after.Quota), info.PriceData.Charge.FinalQuota)
}

func TestTextQuoteUsesCanonicalProviderUsage(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	prices := []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}, {Quantity: "c", Unit: "per_million_tokens", Price: "4"}, {Quantity: "cr", Unit: "per_million_tokens", Price: "0.2"}, {Quantity: "cc", Unit: "per_million_tokens", Price: "2.5"}, {Quantity: "cc1h", Unit: "per_million_tokens", Price: "4"}}
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		info := quotedTextInfo(t, prices)
		info.RelayFormat = format
		usage := &dto.Usage{PromptTokens: 999, CompletionTokens: 999, BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 100, OutputTokens: 10, CacheReadInputTokens: 20, CacheCreationInputTokens: 30, ClaudeCacheCreation5mTokens: 10, ClaudeCacheCreation1hTokens: 20})}
		summary := calculateTextQuotaSummary(ctx, info, effectiveBillingUsage(usage))
		require.NoError(t, summary.PricingError)
		assert.Equal(t, 100, summary.PromptTokens)
		assert.Equal(t, int64(150), info.PriceData.Usage.ContextTokens)
		assert.Equal(t, 175, summary.Quota)
	}
	for _, provider := range []*dto.BillingUsage{
		dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 80, CompletionTokens: 9}),
		dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 80, CandidatesTokenCount: 9}),
	} {
		info := quotedTextInfo(t, prices)
		summary := calculateTextQuotaSummary(ctx, info, effectiveBillingUsage(&dto.Usage{PromptTokens: 999, BillingUsage: provider}))
		require.NoError(t, summary.PricingError)
		assert.Equal(t, 98, summary.Quota)
	}
}

func TestTextQuoteCacheOverlapCannotCredit(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}, {Quantity: "cr", Unit: "per_million_tokens", Price: "1"}, {Quantity: "cc", Unit: "per_million_tokens", Price: "2"}})
	usage := &dto.Usage{PromptTokens: 100, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: 80, CachedCreationTokens: 60}}
	summary := calculateTextQuotaSummary(ctx, info, usage)
	require.NoError(t, summary.PricingError)
	assert.Equal(t, 100, summary.Quota)
}

func TestTextToolQuoteBillsZeroTokenUsageAndFrozenPrices(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("claude_web_search_requests", 2)
	info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "tool:web_search", Unit: "per_thousand_calls", Price: "10"}, {Quantity: "tool:google_search", Unit: "per_call", Price: "0.014"}})
	ctx.Set("gemini_google_search_call", true)
	summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.NoError(t, summary.PricingError)
	assert.Equal(t, 17000, summary.Quota)
	assert.True(t, summary.hasBillableUsage())
	require.Len(t, summary.ToolSurchargeItems, 2)
	info.OriginModelName = "unrelated-search-preview"
	info.RelayMode = relayconstant.RelayModeResponses
	summary = calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.NoError(t, summary.PricingError)
	assert.Equal(t, 17000, summary.Quota)
	info.RelayMode = relayconstant.RelayModeChatCompletions
	summary = calculateTextQuotaSummary(ctx, info, &dto.Usage{})
	require.ErrorContains(t, summary.PricingError, "missing price")
}

func TestTextQuoteRejectsNegativeCacheBeforeDisplayNormalization(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := quotedTextInfo(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "2"}})
	usage := &dto.Usage{PromptTokens: 100, PromptTokensDetails: dto.InputTokenDetails{CachedCreationTokens: -1, CacheWriteTokens: 20}}
	summary := calculateTextQuotaSummary(ctx, info, usage)
	require.ErrorContains(t, summary.PricingError, "invalid provider")
	assert.Zero(t, summary.Quota)
	assert.True(t, info.PriceData.Usage.Invalid)
}

func TestTextMissingQuoteCannotUseOldRatioOrPrice(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{PriceData: hosttypes.PriceData{}}
	summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{PromptTokens: 100})
	require.ErrorContains(t, summary.PricingError, "frozen billing quote is required")
	assert.Zero(t, summary.Quota)
}

func TestEmbeddingScopeKeepsOutputEvidenceAcrossUsageAliases(t *testing.T) {
	for _, test := range []struct {
		name   string
		output dto.Usage
	}{
		{"aggregate", dto.Usage{CompletionTokens: 1}},
		{"aggregate_alias", dto.Usage{OutputTokens: 1}},
		{"text_detail", dto.Usage{CompletionTokenDetails: dto.OutputTokenDetails{TextTokens: 1}}},
		{"reasoning_detail", dto.Usage{CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 1}}},
		{"audio_detail", dto.Usage{CompletionTokenDetails: dto.OutputTokenDetails{AudioTokens: 1}}},
		{"image_detail", dto.Usage{CompletionTokenDetails: dto.OutputTokenDetails{ImageTokens: 1}}},
		{"detail_alias", dto.Usage{OutputTokensDetails: &dto.OutputTokenDetails{ReasoningTokens: 1}}},
		{"invalid_detail_alias", dto.Usage{OutputTokensDetails: &dto.OutputTokenDetails{AudioTokens: -1}}},
		{"raw_detail_with_canonical_zero", dto.Usage{CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 1}, BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 1000})}},
		{"canonical_detail", dto.Usage{BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 1000, CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 1}})}},
		{"canonical_conflicting_details", dto.Usage{BillingUsage: &dto.BillingUsage{Source: dto.BillingUsageSourceOAIChat, Semantic: dto.BillingUsageSemanticOpenAI, OpenAIUsage: &dto.Usage{PromptTokens: 1000, CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 1}, OutputTokensDetails: &dto.OutputTokenDetails{}}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			quote, err := billing.NewQuote(billing.PriceVersion{Model: "embedding-test", Mode: billing.ModeExpression, Expression: "p * 1", UsageScope: billing.UsageScopeEmbedding}, "1", nil, time.Now())
			require.NoError(t, err)
			info := &relaycommon.RelayInfo{OriginModelName: "embedding-test", StartTime: time.Now(), PriceData: hosttypes.PriceData{Quote: quote}}
			usage := test.output
			usage.PromptTokens, usage.UsageSource = 1000, "provider"
			summary := calculateTextQuotaSummary(ctx, info, &usage)
			require.Error(t, summary.PricingError)
			require.NotNil(t, info.PriceData.Usage)
			assert.True(t, info.PriceData.Usage.Invalid, "persisted evidence must remain invalid during replay")
			assert.Zero(t, summary.Quota)
			_, err = billing.Calculate(quote, *info.PriceData.Usage)
			require.Error(t, err)
		})
	}

	t.Run("unscoped_manual_price", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		quote, err := billing.NewQuote(billing.PriceVersion{Model: "manual", Mode: billing.ModeExpression, Expression: "p * 1"}, "1", nil, time.Now())
		require.NoError(t, err)
		info := &relaycommon.RelayInfo{OriginModelName: "manual", StartTime: time.Now(), PriceData: hosttypes.PriceData{Quote: quote}}
		summary := calculateTextQuotaSummary(ctx, info, &dto.Usage{PromptTokens: 1000, CompletionTokens: 5, CompletionTokenDetails: dto.OutputTokenDetails{ReasoningTokens: 5}, UsageSource: "provider"})
		require.NoError(t, summary.PricingError)
		assert.Equal(t, 500, summary.Quota)
		assert.False(t, info.PriceData.Usage.Invalid)
	})
}

func TestUsageBillingPathForLog(t *testing.T) {
	require.Equal(t, usageBillingPathLocal, usageBillingPathForLog(false, &dto.Usage{UsageSource: "estimated"}))
	require.Equal(t, usageBillingPathMixed, usageBillingPathForLog(false, &dto.Usage{UsageSource: "provider_and_estimated"}))
	require.Equal(t, usageBillingPathAnthropic, usageBillingPathForLog(true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	invalidBillingUsage := &dto.Usage{
		PromptTokens: 1,
		BillingUsage: &dto.BillingUsage{
			Source:   dto.BillingUsageSourceClaudeMessages,
			Semantic: dto.BillingUsageSemanticAnthropic,
		},
	}
	require.Equal(t, usageBillingPathLocal, usageBillingPathForLog(true, invalidBillingUsage))
	require.Equal(t, usageBillingPathUpstream, usageBillingPathForLog(false, invalidBillingUsage))
	require.Equal(t, usageBillingPathUpstream, usageBillingPathForLog(false, &dto.Usage{}))
	require.Equal(t, usageBillingPathOpenAI, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
	require.Equal(t, usageBillingPathAnthropic, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	}))
	require.Equal(t, usageBillingPathGemini, usageBillingPathForLog(false, &dto.Usage{
		BillingUsage: dto.NewGeminiChatBillingUsage(&dto.GeminiUsageMetadata{PromptTokenCount: 1}),
	}))
	require.Equal(t, usageBillingPathGeminiEstimated, usageBillingPathForLog(true, &dto.Usage{
		BillingUsage: dto.NewEstimatedGeminiChatBillingUsage(&dto.Usage{PromptTokens: 1}),
	}))
}

func TestAppendUsageBillingPathForLogWritesAdminInfo(t *testing.T) {
	other := map[string]interface{}{
		"admin_info": map[string]interface{}{},
	}
	appendUsageBillingPathForLog(other, true, &dto.Usage{
		BillingUsage: dto.NewClaudeMessagesBillingUsage(&dto.ClaudeUsage{InputTokens: 1}),
	})

	adminInfo, ok := other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, usageBillingPathAnthropic, adminInfo["usage_billing_path"])

	other = map[string]interface{}{}
	appendUsageBillingPathForLog(other, true, nil)
	adminInfo, ok = other["admin_info"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, usageBillingPathLocal, adminInfo["usage_billing_path"])
}

func TestCacheWriteTokensTotal(t *testing.T) {
	t.Run("split cache creation", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens:   50,
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("legacy cache creation", func(t *testing.T) {
		summary := textQuotaSummary{CacheCreationTokens: 50}
		require.Equal(t, 50, cacheWriteTokensTotal(summary))
	})

	t.Run("split cache creation without aggregate remainder", func(t *testing.T) {
		summary := textQuotaSummary{
			CacheCreationTokens5m: 10,
			CacheCreationTokens1h: 20,
		}
		require.Equal(t, 30, cacheWriteTokensTotal(summary))
	})
}

func TestAppendToolSurchargeLogInfoWritesOnlyStructuredFields(t *testing.T) {
	items := []ToolSurchargeItem{
		{Name: dto.BuildInToolWebSearch, Count: 2, Price: "10"},
		{Name: dto.BuildInToolImageGeneration, Count: 1, Price: "150"},
	}
	other := map[string]interface{}{}

	appendToolSurchargeLogInfo(other, items)

	assert.Equal(t, items, other["tool_surcharges"])
	assert.NotContains(t, other, "web_search")
	assert.NotContains(t, other, "web_search_call_count")
	assert.NotContains(t, other, "web_search_price")
	assert.NotContains(t, other, "file_search")
	assert.NotContains(t, other, "image_generation_call")
	assert.NotContains(t, other, "image_generation_call_price")
}
