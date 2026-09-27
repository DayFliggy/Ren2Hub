package helper

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pricingFixture(t *testing.T, version billing.PriceVersion) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingPriceVersion{}, &model.BillingActivePrice{}))
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousType, previousLog)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	_, err = model.PublishBillingPrice(context.Background(), version, 0, 1)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("channel_ratio", "1")
	info := &relaycommon.RelayInfo{OriginModelName: version.Model, BillingRequestInput: &billingexpr.RequestInput{Attributes: map[string]interface{}{"service_tier": "priority"}, FrozenAt: time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)}}
	return c, info
}

func TestImageExpressionPrepaymentUsesFinalQuantity(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "image-count", Mode: billing.ModeExpression, Expression: `v1:fixed(0.001) * image_count`})
	info.BillingRequestInput.Attributes["quantity"] = float64(1)
	price, err := ModelPriceHelper(c, info, 0, &types.TokenCountMeta{BillingQuantities: map[string]float64{"n": 3}})
	require.NoError(t, err)
	assert.Equal(t, 1500, price.QuotaToPreConsume)
	assert.Equal(t, "3", price.Usage.Quantities["images"])
	for _, n := range []float64{0, -1, 1.5, 129} {
		_, err := ModelPriceHelper(c, info, 0, &types.TokenCountMeta{BillingQuantities: map[string]float64{"n": n}})
		require.Error(t, err)
	}
	delete(info.BillingRequestInput.Attributes, "quantity")
	_, err = ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.ErrorContains(t, err, "image quantity", "missing final quantity cannot silently default")
}

func TestPublishedPricePrepaymentUsesFrozenAttributesAndDefaultOutputEstimate(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: `p * 2 + c * (attr("service_tier") == "priority" ? 4 : 1)`})
	price, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, 16484, price.QuotaToPreConsume)
	require.NotNil(t, price.Quote)
	assert.Equal(t, 3, price.Quote.FrozenAt.Hour())
	assert.True(t, price.Usage.Estimated)
	previous := price.Quote
	_, err = model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "99"}, {Quantity: "c", Unit: "per_million_tokens", Price: "99"}}}, previous.PriceVersion.ID, 1)
	require.NoError(t, err)
	result, err := billing.Calculate(previous, *price.Usage)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(16484), result.FinalQuota)
}

func TestPublishedPriceRejectsMissingModelOverflowAndInvalidChannelRatio(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "999999999999"}}})
	_, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 1})
	require.Error(t, err)
	info.OriginModelName = "not-published"
	_, err = ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
	require.Error(t, err)
	info.OriginModelName = "test"
	c.Set("channel_ratio", "-1.0")
	_, err = ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
	require.Error(t, err)
}

func TestPublishedPricePreservesExactChannelRatio(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "exact-ratio", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.001"}}})
	c.Set("channel_ratio", "1.234567890123456789")
	price, err := ModelPriceHelperPerCall(c, info)
	require.NoError(t, err)
	assert.Equal(t, "1.234567890123456789", price.Quote.ChannelRatio)
	charge, err := billing.Calculate(price.Quote, *price.Usage)
	require.NoError(t, err)
	assert.Equal(t, "0.001234567890123456789", charge.TotalUSD)
	c.Set("channel_ratio", 1.5)
	_, err = ModelPriceHelperPerCall(c, info)
	require.Error(t, err, "a float context must not become a financial default")
}

func TestPublishedZeroEstimateStillReservesOneUnlessExplicitFree(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0"}}})
	price, err := ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, 1, price.QuotaToPreConsume)
	assert.False(t, price.FreeModel)
	_, err = model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{Model: "test", Mode: billing.ModeFree}, price.Quote.PriceVersion.ID, 1)
	require.NoError(t, err)
	price, err = ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Zero(t, price.QuotaToPreConsume)
	assert.True(t, price.FreeModel)
	require.NotNil(t, price.Quote)
}

func TestPublishedMixedCallAndTokenPricesReserveEveryKnownDimension(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "mixed", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{
		{Quantity: "calls", Unit: "per_call", Price: "0.002"},
		{Quantity: "p", Unit: "per_million_tokens", Price: "2"},
		{Quantity: "c", Unit: "per_million_tokens", Price: "4"},
	}})
	price, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 50})
	require.NoError(t, err)
	assert.Equal(t, 1200, price.QuotaToPreConsume)
	assert.EqualValues(t, 100, price.Usage.InputTokens)
	assert.EqualValues(t, 50, price.Usage.OutputTokens)
}

func TestRetryPinsPublishedPriceAndClockButRepricesFinalAttributes(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: `p * (attr("quality") == "high" ? 4 : 2)`})
	info.RequestId = "logical-request"
	first, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 1})
	require.NoError(t, err)
	require.NoError(t, PinBillingPrice(c, info))
	_, err = model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{Model: "test", Mode: billing.ModeFree}, first.Quote.PriceVersion.ID, 1)
	require.NoError(t, err)
	require.NoError(t, FreezeOutboundBillingAttributes(info, []byte(`{"quality":"high"}`)))
	c.Set("channel_ratio", "2.0")
	next, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 1})
	require.NoError(t, err)
	assert.Equal(t, first.Quote.PriceVersion.ID, next.Quote.PriceVersion.ID)
	assert.Equal(t, first.Quote.FrozenAt, next.Quote.FrozenAt)
	assert.Equal(t, 400, next.QuotaToPreConsume)
	assert.False(t, next.FreeModel)
	info.OriginModelName = "another"
	_, err = ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 1})
	require.ErrorContains(t, err, "identity")
}

func TestChannelRetryFreezesIndependentVersionsAndReserveEstimates(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, FlatFee: "0.001"})
	info.RequestId, info.ChannelMeta = "scoped-retry", &relaycommon.ChannelMeta{ChannelId: 7}
	c.Set("channel_ratio", "2.0")
	first, err := ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.NoError(t, PinBillingPrice(c, info))
	assert.Equal(t, 1000, first.QuotaToPreConsume)
	scoped, err := model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{ScopeKind: billing.PriceScopeChannel, ChannelID: 19, Model: "test", Mode: billing.ModeUnitPrice, FlatFee: "0.003"}, 0, 1)
	require.NoError(t, err)
	info.ChannelId = 19
	next, err := ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.NoError(t, PinBillingPrice(c, info))
	assert.Equal(t, scoped.ID, next.Quote.PriceVersion.ID)
	assert.Equal(t, "1", next.Quote.ChannelRatio)
	assert.Equal(t, 1500, next.QuotaToPreConsume)
	assert.Equal(t, first.Quote.FrozenAt, next.Quote.FrozenAt)
	assert.NotEqual(t, first.Quote.Hash, next.Quote.Hash)
	_, err = model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{ScopeKind: billing.PriceScopeChannel, ChannelID: 19, Model: "test", Mode: billing.ModeFree}, scoped.ID, 1)
	require.NoError(t, err)
	again, err := ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, next.Quote.Hash, again.Quote.Hash)
	info.ChannelId = 7
	again, err = ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, first.Quote.Hash, again.Quote.Hash)
}

func TestReasoningPrepaymentUsesMoreExpensiveOutputEstimate(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{
		{Quantity: "p", Unit: "per_million_tokens", Price: "1"},
		{Quantity: "c", Unit: "per_million_tokens", Price: "2"},
		{Quantity: "reasoning", Unit: "per_million_tokens", Price: "10"},
	}})
	price, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 20})
	require.NoError(t, err)
	assert.Equal(t, 150, price.QuotaToPreConsume)
	assert.EqualValues(t, 20, price.Usage.ReasoningTokens)
}

func TestAutomaticUnavailablePriceStopsFallbackUntilManualPublication(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "gpt-5.4", Mode: billing.ModeExpression, Expression: "p + c"})
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high@thinking:on"} {
		require.NoError(t, model.DB.Create(&model.BillingActivePrice{Model: name, Origin: "unavailable"}).Error)
	}
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high"} {
		info.OriginModelName = name
		assert.False(t, HasModelBillingConfig(name), name)
		assert.False(t, HasExplicitModelBillingConfig(name), name)
		_, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{MaxTokens: 1})
		require.ErrorContains(t, err, "Model price is not configured", name)
		assert.Nil(t, info.PriceData.Quote)
	}
	assert.True(t, HasModelBillingConfig("gpt-5.4"), "an unavailable alias cannot disable a valid base price")
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high@thinking:on"} {
		_, err := model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{Model: name, Mode: billing.ModeExpression, Expression: "p * 3"}, 0, 1)
		require.NoError(t, err)
	}
	for _, name := range []string{"gpt-5.4-high", "gpt-5.4@effort:high"} {
		info.OriginModelName, info.PricingModelName = name, ""
		assert.True(t, HasModelBillingConfig(name), name)
		price, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{MaxTokens: 1})
		require.NoError(t, err)
		assert.Equal(t, 1500, price.QuotaToPreConsume)
	}
}

func TestPinnedQuoteSurvivesAutomaticPriceBecomingUnavailable(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "gpt-5.4-high", Mode: billing.ModeExpression, Expression: "p * 3"})
	info.RequestId = "already-dispatched"
	first, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{MaxTokens: 1})
	require.NoError(t, err)
	require.NoError(t, PinBillingPrice(c, info))
	require.NoError(t, model.DB.Model(&model.BillingActivePrice{}).Where("model = ?", "gpt-5.4-high").Updates(map[string]any{"version_id": 0, "origin": "unavailable"}).Error)
	assert.False(t, HasModelBillingConfig("gpt-5.4-high"))
	next, err := ModelPriceHelper(c, info, 1000, &types.TokenCountMeta{MaxTokens: 1})
	require.NoError(t, err)
	assert.Equal(t, first.Quote.Hash, next.Quote.Hash)
	charge, err := billing.Calculate(first.Quote, billing.Usage{InputTokens: 1000, ContextTokens: 1000, Complete: true, InputIncludesCache: true})
	require.NoError(t, err)
	assert.EqualValues(t, 1500, charge.FinalQuota)
}

func TestEmbeddingScopedPriceRejectsOtherOperationsBeforeReservation(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "embedding", Mode: billing.ModeExpression, Expression: `v1:p * 0.1`, UsageScope: billing.UsageScopeEmbedding})
	for _, mode := range []int{relayconstant.RelayModeChatCompletions, relayconstant.RelayModeResponses, relayconstant.RelayModeGemini, relayconstant.RelayModeRerank, relayconstant.RelayModeImagesGenerations} {
		info.RelayMode = mode
		price, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
		require.ErrorContains(t, err, "request type")
		assert.Nil(t, price.Quote)
		assert.Nil(t, info.PriceData.Quote)
		assert.False(t, info.UpstreamSent)
	}
}

func TestEmbeddingScopedPriceAllowsOpenAIAndNativeGeminiWithoutOutputEstimate(t *testing.T) {
	for _, test := range []struct {
		name    string
		path    string
		request dto.Request
	}{
		{"openai", "/v1/embeddings", &dto.EmbeddingRequest{Model: "embedding", Input: "hello"}},
		{"gemini", "/v1beta/models/embedding:embedContent", &dto.GeminiEmbeddingRequest{}},
		{"gemini_batch", "/v1beta/models/embedding:batchEmbedContents", &dto.GeminiBatchEmbeddingRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, info := pricingFixture(t, billing.PriceVersion{Model: "embedding", Mode: billing.ModeExpression, Expression: `v1:p * 0.1`, UsageScope: billing.UsageScopeEmbedding})
			c.Request = httptest.NewRequest("POST", test.path, nil)
			info.RelayMode = relayconstant.Path2RelayMode(test.path)
			info.Request = test.request
			price, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 8192})
			require.NoError(t, err)
			assert.Equal(t, 5, price.QuotaToPreConsume)
			assert.Zero(t, price.Usage.OutputTokens)
			assert.Empty(t, price.Usage.Quantities)
			assert.Equal(t, billing.UsageScopeEmbedding, price.Quote.PriceVersion.UsageScope)
			info.RequestId = "embedding-request"
			require.NoError(t, PinBillingPrice(c, info))
			info.RelayMode = relayconstant.RelayModeChatCompletions
			_, err = ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
			require.ErrorContains(t, err, "request type", "pinned retry keeps the original scope")
		})
	}
}
