package billing_test

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quoteFor(t *testing.T, prices []billing.UnitPrice) *billing.Quote {
	t.Helper()
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: prices}, "1", nil, time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	return quote
}

func TestCalculateRoundsTotalOnceAndPreservesTinyPositiveCharge(t *testing.T) {
	q := quoteFor(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "0.6"}, {Quantity: "c", Unit: "per_million_tokens", Price: "0.6"}})
	result, err := billing.Calculate(q, billing.Usage{InputTokens: 1, OutputTokens: 1, ContextTokens: 1, InputIncludesCache: true})
	require.NoError(t, err)
	assert.Equal(t, common.Quota(1), result.FinalQuota)
	assert.Equal(t, "0.6", result.UnroundedQuota)
	assert.Equal(t, "0.4", result.RoundingDelta)
	require.Len(t, result.Lines, 2)
	for _, price := range []string{"0", "0.000000000000000001"} {
		q = quoteFor(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: price}})
		result, err = billing.Calculate(q, billing.Usage{InputTokens: 1, ContextTokens: 1, InputIncludesCache: true})
		require.NoError(t, err)
		if price == "0" {
			assert.Zero(t, result.FinalQuota)
		} else {
			assert.Equal(t, common.Quota(1), result.FinalQuota)
		}
	}
}

func TestCalculateProtocolNormalizationAndContextLength(t *testing.T) {
	q := quoteFor(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "3"}, {Quantity: "c", Unit: "per_million_tokens", Price: "15"}, {Quantity: "cr", Unit: "per_million_tokens", Price: "0.3"}, {Quantity: "cc", Unit: "per_million_tokens", Price: "3.75"}, {Quantity: "cc1h", Unit: "per_million_tokens", Price: "6"}})
	oai := billing.Usage{InputTokens: 1000, OutputTokens: 10, ContextTokens: 1000, CacheReadTokens: 500, CacheWriteTokens: 100, CacheWrite1hTokens: 100, InputIncludesCache: true}
	claude := oai
	claude.InputTokens = 300
	claude.InputIncludesCache = false
	one, err := billing.Calculate(q, oai)
	require.NoError(t, err)
	two, err := billing.Calculate(q, claude)
	require.NoError(t, err)
	assert.Equal(t, one, two)
	assert.Equal(t, common.Quota(1088), one.FinalQuota)
	q, err = billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: `len == 1000 ? p * 3 : p * 999`}, "1", nil, time.Now())
	require.NoError(t, err)
	one, err = billing.Calculate(q, oai)
	require.NoError(t, err)
	two, err = billing.Calculate(q, claude)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(1500), one.FinalQuota)
	assert.Equal(t, one.FinalQuota, two.FinalQuota, "unpriced cache categories stay in input base")
}

func TestCalculateUsesFrozenAllPricesAndRejectsUnknownCharges(t *testing.T) {
	prices := []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}, {Quantity: "ai", Unit: "per_million_tokens", Price: "10"}, {Quantity: "tool:web_search", Unit: "per_thousand_calls", Price: "10"}}
	q := quoteFor(t, prices)
	prices[1].Price = "999"
	usage := billing.Usage{InputTokens: 100, ContextTokens: 100, AudioInputTokens: 40, InputIncludesCache: true, Quantities: map[string]string{"tool:web_search": "2"}}
	result, err := billing.Calculate(q, usage)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(10230), result.FinalQuota)
	usage.Quantities["tool:unknown"] = "1"
	_, err = billing.Calculate(q, usage)
	require.ErrorContains(t, err, "missing price")
	delete(usage.Quantities, "tool:unknown")
	q.PriceVersion.Prices[0].Price = "5"
	_, err = billing.Calculate(q, usage)
	require.ErrorContains(t, err, "integrity")
}

func TestCalculateFailsClosedForInvalidAmountsAndUsage(t *testing.T) {
	q := quoteFor(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "99999999999999"}})
	_, err := billing.Calculate(q, billing.Usage{InputTokens: 100, InputIncludesCache: true})
	require.ErrorContains(t, err, "request limit")
	_, err = billing.Calculate(q, billing.Usage{InputTokens: -1})
	require.ErrorContains(t, err, "quantity")
	_, err = billing.Calculate(nil, billing.Usage{})
	require.ErrorContains(t, err, "missing")
	for _, price := range []string{"NaN", "-1", "1e-99", "1e999999"} {
		err := billing.ValidatePriceVersion(billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: price}}})
		require.Error(t, err, price)
	}
	q = quoteFor(t, []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}})
	_, err = billing.Calculate(q, billing.Usage{OutputTokens: 1})
	require.ErrorContains(t, err, "missing price")
	limit, err := billing.RoundCharge(decimal.NewFromInt(math.MaxInt32))
	require.NoError(t, err)
	assert.Equal(t, common.Quota(math.MaxInt32), limit)
}

func TestExpressionFrozenAttributeTimeAndCombinedToolRounding(t *testing.T) {
	at := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	attrs := map[string]interface{}{"quality": "high"}
	q, err := billing.NewQuote(billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: `p * (attr("quality") == "high" && hour("UTC") < 6 ? 0.6 : 100)`, Prices: []billing.UnitPrice{{Quantity: "tool:lookup", Unit: "per_call", Price: "0.0000006"}}}, "1", attrs, at)
	require.NoError(t, err)
	attrs["quality"] = "low"
	u := billing.Usage{InputTokens: 1, ContextTokens: 1, InputIncludesCache: true, Quantities: map[string]string{"tool:lookup": "1"}}
	one, err := billing.Calculate(q, u)
	require.NoError(t, err)
	two, err := billing.Calculate(q, u)
	require.NoError(t, err)
	assert.Equal(t, one, two)
	assert.Equal(t, "0.6", one.UnroundedQuota)
	assert.Equal(t, common.Quota(1), one.FinalQuota)
	assert.Equal(t, 1, one.ExpressionVersion)
}

func TestExpressionCanPriceReasoningTokensIndependently(t *testing.T) {
	quote, err := billing.NewQuote(billing.PriceVersion{
		Model: "reasoning-expression", Mode: billing.ModeExpression,
		Expression: `p * 1 + c * 2 + reasoning * 10`,
	}, "1", nil, time.Now())
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{
		InputTokens: 100, OutputTokens: 20, ReasoningTokens: 5,
		ContextTokens: 100, InputIncludesCache: true,
	})
	require.NoError(t, err)
	assert.Equal(t, common.Quota(90), charge.FinalQuota)
}

func TestTieredPriceModesAndFlatFee(t *testing.T) {
	volume, err := billing.NewQuote(billing.PriceVersion{
		Model: "meter", Mode: billing.ModeUnitPrice, FlatFee: "0.01",
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", TierMode: billing.TierModeVolume, Tiers: []billing.PriceTier{{UpTo: "10", UnitPrice: "0.02"}, {UnitPrice: "0.01"}}}},
	}, "1", nil, time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	charge, err := billing.Calculate(volume, billing.Usage{Quantities: map[string]string{"calls": "12"}})
	require.NoError(t, err)
	assert.Equal(t, "0.13", charge.TotalUSD)
	assert.Equal(t, common.Quota(65000), charge.FinalQuota)
	assert.Len(t, charge.Lines, 2)

	graduated, err := billing.NewQuote(billing.PriceVersion{
		Model: "meter", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", TierMode: billing.TierModeGraduated, Tiers: []billing.PriceTier{{UpTo: "10", UnitPrice: "0.02"}, {UnitPrice: "0.01"}}}},
	}, "1", nil, time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	charge, err = billing.Calculate(graduated, billing.Usage{Quantities: map[string]string{"calls": "12"}})
	require.NoError(t, err)
	assert.Equal(t, "0.22", charge.TotalUSD)
	assert.Equal(t, common.Quota(110000), charge.FinalQuota)
	assert.Len(t, charge.Lines, 2)

	finite, err := billing.NewQuote(billing.PriceVersion{
		Model: "meter", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", TierMode: billing.TierModeVolume, Tiers: []billing.PriceTier{{UpTo: "10", UnitPrice: "0.02"}}}},
	}, "1", nil, time.Now())
	require.NoError(t, err)
	_, err = billing.Calculate(finite, billing.Usage{Quantities: map[string]string{"calls": "11"}})
	require.ErrorContains(t, err, "exceeds tier range")
}

func TestReasoningTokensUseAnIndependentFrozenUnitPrice(t *testing.T) {
	quote := quoteFor(t, []billing.UnitPrice{{Quantity: "reasoning", Unit: "per_million_tokens", Price: "2"}})
	charge, err := billing.Calculate(quote, billing.Usage{OutputTokens: 20, ReasoningTokens: 20})
	require.NoError(t, err)
	assert.Equal(t, "0.00004", charge.TotalUSD)
	assert.Equal(t, common.Quota(20), charge.FinalQuota)

	_, err = billing.Calculate(quote, billing.Usage{OutputTokens: 19, ReasoningTokens: 20})
	require.ErrorContains(t, err, "reasoning token quantity")
}

func TestPriceWindowAndExpressionOverflowFailClosed(t *testing.T) {
	start := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	_, err := billing.NewQuote(billing.PriceVersion{
		Model: "windowed", Mode: billing.ModeUnitPrice,
		EffectiveFrom: &start, EffectiveTo: &end,
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "1"}},
	}, "1", nil, start.Add(-time.Nanosecond))
	require.ErrorContains(t, err, "not yet effective")

	quote, err := billing.NewQuote(billing.PriceVersion{Model: "overflow", Mode: billing.ModeExpression, Expression: `v1:p * 9007199254740992`}, "1", nil, start)
	require.NoError(t, err)
	_, err = billing.Calculate(quote, billing.Usage{InputTokens: 1, ContextTokens: 1, InputIncludesCache: true})
	require.ErrorContains(t, err, "request limit")

	quote, err = billing.NewQuote(billing.PriceVersion{Model: "precision", Mode: billing.ModeExpression, Expression: `v1:p * 0.0000000000000000001`}, "1", nil, start)
	require.NoError(t, err)
	_, err = billing.Calculate(quote, billing.Usage{InputTokens: 1, ContextTokens: 1, InputIncludesCache: true})
	require.ErrorContains(t, err, "decimal range")
}

func TestQuoteOwnsTierAndWindowCopies(t *testing.T) {
	start := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	prices := []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", TierMode: billing.TierModeVolume, Tiers: []billing.PriceTier{{UnitPrice: "0.002"}}}}
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "frozen", Mode: billing.ModeUnitPrice, Prices: prices, EffectiveFrom: &start, EffectiveTo: &end}, "1", nil, start)
	require.NoError(t, err)
	prices[0].Tiers[0].UnitPrice = "99"
	start, end = end.Add(time.Hour), start.Add(-time.Hour)
	charge, err := billing.Calculate(quote, billing.Usage{Quantities: map[string]string{"calls": "1"}})
	require.NoError(t, err)
	assert.Equal(t, "0.002", charge.TotalUSD)
}
