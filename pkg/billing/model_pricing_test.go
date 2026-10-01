package billing_test

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestOnlyUpstreamModelPricingCanCreateAQuote(t *testing.T) {
	for _, mode := range []string{"unit_price", "free", billing.ModeExpression} {
		_, err := billing.NewQuote(billing.PriceVersion{Model: "old", Mode: mode, Expression: "p"}, "1", nil, time.Now())
		require.Error(t, err)
	}
	values := billing.PricingValues{"ModelRatio": 2.0, "CompletionRatio": 3.0}
	version, err := billing.NewModelPriceVersion("immutable", values, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	values["ModelRatio"] = 99.0
	version.Pricing["CompletionRatio"] = 99.0
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 10, OutputTokens: 10, InputIncludesCache: true})
	require.NoError(t, err)
	assert.EqualValues(t, 80, charge.FinalQuota)
	quote.PriceVersion.Pricing["ModelRatio"] = 99.0
	_, err = billing.Calculate(quote, billing.Usage{InputTokens: 10})
	require.Error(t, err)
}

func TestPricingRejectsUnrepresentableChargesAndInvalidUsage(t *testing.T) {
	version, err := billing.NewModelPriceVersion("bounded", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p*1e30"}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	_, err = billing.Calculate(quote, billing.Usage{InputTokens: 1, InputIncludesCache: true})
	require.ErrorContains(t, err, "request limit")
	for _, usage := range []billing.Usage{{InputTokens: -1}, {InputTokens: 100, ImageInputTokens: 80, AudioInputTokens: 80, InputIncludesCache: true}, {OutputTokens: 100, ImageOutputTokens: 80, AudioOutputTokens: 80}} {
		_, err = billing.Calculate(quote, usage)
		require.Error(t, err)
	}
}

func TestTaskExpressionsUseUSDWithoutFixedConversion(t *testing.T) {
	values := billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("seconds")*0.4`}
	version, err := billing.NewModelPriceVersion("task", values, true)
	require.NoError(t, err)
	assert.Equal(t, billing.ResultUSD, version.ResultUnit)
	values["billing_setting.billing_expr"] = `tier("task", fixed(0.4))`
	_, err = billing.NewModelPriceVersion("task", values, true)
	require.ErrorContains(t, err, "fixed pricing is not supported for task")
}

func TestTaskExpressionReadsOnlyTaskFactsDespiteProviderTokens(t *testing.T) {
	version, err := billing.NewModelPriceVersion("task", billing.PricingValues{
		"billing_setting.billing_mode": "tiered_expr",
		"billing_setting.billing_expr": `u("count") + p`,
	}, true)
	require.NoError(t, err)
	assert.False(t, version.NeedsTokenUsage())
	assert.Equal(t, billing.ResultUSD, version.ResultUnit)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	cipher, err := billing.NewEvidenceCipher([]byte("task-token-isolation-test-key"))
	require.NoError(t, err)
	binding := billing.EvidenceBinding{RequestID: "task-request", AttemptID: "task-attempt", ConfigHash: billing.PricingConfigHash(quote), Phase: "settlement"}
	usage, charge, err := cipher.Evaluate(quote, billing.Usage{InputTokens: 1000, ContextTokens: 1000, InputIncludesCache: true, Complete: true}, billingexpr.RequestInput{Usage: map[string]any{"count": 0.25}}, binding)
	require.NoError(t, err)
	assert.Equal(t, "0.25", charge.TotalUSD)
	assert.EqualValues(t, 125000, charge.FinalQuota)
	replayed, err := cipher.Replay(quote, usage, binding)
	require.NoError(t, err)
	assert.Equal(t, charge, replayed)
}

func TestCacheImageBreakdownAndClaudeContextUseUpstreamNormalization(t *testing.T) {
	version, err := billing.NewModelPriceVersion("image", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `p*5+cr*1.25+img*8+img_cr*2+c*30`}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	cached := int64(200)
	usage := billing.Usage{InputTokens: 1000, OutputTokens: 100, ImageInputTokens: 600, CacheReadTokens: 300, ImageCacheReadTokens: &cached, InputIncludesCache: true}
	charge, err := billing.Calculate(quote, usage)
	require.NoError(t, err)
	assert.Equal(t, "0.008225", charge.TotalUSD)
	assert.EqualValues(t, 4113, charge.FinalQuota)
	usage.ImageCacheReadTokens = nil
	charge, err = billing.Calculate(quote, usage)
	require.NoError(t, err)
	assert.Equal(t, "0.008675", charge.TotalUSD)
	for _, count := range []int64{-1, 301, 1001} {
		usage.ImageCacheReadTokens = &count
		_, err = billing.Calculate(quote, usage)
		require.Error(t, err)
	}
	claude := billing.Usage{InputTokens: 20, CacheReadTokens: 50, CacheWriteTokens: 30, CacheWrite1hTokens: 10}
	params, err := claude.TokenParams(nil)
	require.NoError(t, err)
	assert.EqualValues(t, 70, params.P)
	assert.EqualValues(t, 110, params.Len)
}

func TestFrozenModifiersApplyOnceAndOnlyToTheirPhase(t *testing.T) {
	version, err := billing.NewModelPriceVersion("image", billing.PricingValues{"ModelPrice": 0.04}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1.5", nil, time.Now())
	require.NoError(t, err)
	quote.PreConsumeMultiplier = "10"
	quote.Hash = ""
	quote.Hash, err = billing.Hash(quote)
	require.NoError(t, err)
	cipher, err := billing.NewEvidenceCipher([]byte("test-key"))
	require.NoError(t, err)
	bind := billing.EvidenceBinding{RequestID: "r", AttemptID: "a", ConfigHash: billing.PricingConfigHash(quote), Phase: "estimate"}
	_, charge, err := cipher.Evaluate(quote, billing.Usage{Estimated: true, Quantities: map[string]string{"images": "2"}}, billingexpr.RequestInput{Body: []byte(`{"model":"dall-e-3","size":"1792x1024","quality":"hd"}`)}, bind)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(180000), charge.FinalQuota, "two images, size/quality 3x, channel 1.5x; no token reservation factor")
	assert.Equal(t, float64(3), billing.LegacyImagePriceRatio("dall-e-3", "1792x1024", "hd"))
}

func TestCanonicalUsageConfirmationRemainsIndependentFromPrice(t *testing.T) {
	usage := billing.Usage{Source: "provider", Terminal: "completed", Complete: true}
	assert.True(t, usage.Confirmed())
	assert.True(t, usage.Settleable())
	for _, estimated := range []billing.Usage{{Source: "provider", Terminal: "completed"}, {Source: "provider", Terminal: "completed", Complete: true, Estimated: true}, {Source: "provider", Terminal: "failed", Complete: true}} {
		assert.False(t, estimated.Confirmed())
		assert.True(t, estimated.Settleable(), "best available evidence settles as an estimate")
	}
	for _, invalid := range []billing.Usage{{Terminal: "completed", Complete: true}, {Source: "provider", Terminal: "completed", Complete: true, Invalid: true}} {
		assert.False(t, invalid.Confirmed())
		assert.False(t, invalid.Settleable())
	}
}

func TestPreConsumeMultiplierOnlySizesTheReservation(t *testing.T) {
	version, err := billing.NewModelPriceVersion("ratio-model", billing.PricingValues{"ModelRatio": 1.0}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	quote.PreConsumeMultiplier = "2"
	quote.Hash = ""
	quote.Hash, err = billing.Hash(quote)
	require.NoError(t, err)
	cipher, err := billing.NewEvidenceCipher([]byte("test-key"))
	require.NoError(t, err)
	evaluate := func(source, phase string) *billing.ChargeResult {
		usage := billing.Usage{Source: source, Estimated: true, InputTokens: 1000, ContextTokens: 1000, InputIncludesCache: true, Quantities: map[string]string{}}
		bind := billing.EvidenceBinding{RequestID: "r", AttemptID: "a", ConfigHash: billing.PricingConfigHash(quote), Phase: phase}
		_, charge, err := cipher.Evaluate(quote, usage, billingexpr.RequestInput{}, bind)
		require.NoError(t, err)
		return charge
	}
	reservation := evaluate(billing.UsageSourceRequestEstimate, "estimate")
	settlement := evaluate("estimated", "settlement")
	require.Positive(t, settlement.FinalQuota)
	assert.Equal(t, 2*settlement.FinalQuota, reservation.FinalQuota)
}

func TestPricingRequirementsUseUpstreamModalitiesAndExplicitToolPrices(t *testing.T) {
	for _, values := range []billing.PricingValues{{"ModelRatio": 1.0}, {"ModelPrice": 0.1}, {"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `p*(param("service_tier")=="priority"?2:1)+c`}} {
		version, err := billing.NewModelPriceVersion("modal", values, false)
		require.NoError(t, err)
		require.NoError(t, billing.ValidatePricingRequirements(version, billing.PricingRequirements{Dimensions: []string{"ai", "ao", "cr", "img"}, ServiceTier: "priority"}))
		require.ErrorContains(t, billing.ValidatePricingRequirements(version, billing.PricingRequirements{Dimensions: []string{"tool:search"}}), "missing_price_dimension")
		version.Prices = []billing.UnitPrice{{Quantity: "tool:search", Unit: "per_thousand_calls", Price: "0.001"}}
		require.NoError(t, billing.ValidatePricingRequirements(version, billing.PricingRequirements{Dimensions: []string{"tool:search"}}))
	}
}

func TestExplicitAudioPriceAndToolSurchargeHaveNoHiddenDefaults(t *testing.T) {
	version, err := billing.NewModelPriceVersion("audio", billing.PricingValues{"ModelRatio": 2.0, "AudioRatio": 4.0, "CompletionRatio": 1.0}, false)
	require.NoError(t, err)
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, AudioInputTokens: 25, InputIncludesCache: true})
	require.NoError(t, err)
	assert.EqualValues(t, 350, charge.FinalQuota)
	_, err = billing.Calculate(quote, billing.Usage{InputTokens: 100, Quantities: map[string]string{"tool:search": "1"}})
	require.ErrorContains(t, err, "missing price")
	version, err = billing.NewModelPriceVersion("tool", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p*0.6"}, false)
	require.NoError(t, err)
	version.Prices = []billing.UnitPrice{{Quantity: "tool:search", Unit: "per_thousand_calls", Price: "0.0006"}}
	quote, err = billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	charge, err = billing.Calculate(quote, billing.Usage{InputTokens: 1, InputIncludesCache: true, Quantities: map[string]string{"tool:search": "1"}})
	require.NoError(t, err)
	assert.Equal(t, "0.6", charge.UnroundedQuota)
	assert.Zero(t, charge.FinalQuota, "upstream expression-plus-tool path truncates the combined quota")
}
