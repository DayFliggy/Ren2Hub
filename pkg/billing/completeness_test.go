package billing_test

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixedFeeDoesNotHideUnpricedToolUsage(t *testing.T) {
	for _, mode := range []string{billing.ModeUnitPrice, billing.ModeExpression} {
		price := billing.PriceVersion{Model: "fixed", Mode: mode, FlatFee: "0.000006"}
		if mode == billing.ModeExpression {
			price.Expression = "p * 0.7"
		}
		quote, err := billing.NewQuote(price, "1", nil, time.Unix(7200, 0))
		require.NoError(t, err)
		_, err = billing.Calculate(quote, billing.Usage{Source: "provider", Complete: true, Terminal: "completed", Quantities: map[string]string{"tool:web_search": "7"}})
		require.ErrorContains(t, err, "missing price for quantity tool:web_search")
	}
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "fixed", Mode: billing.ModeUnitPrice, FlatFee: "0.000006"}, "1", nil, time.Unix(7200, 0))
	require.NoError(t, err)
	charge, err := billing.Calculate(quote, billing.Usage{Source: "business_completion", Complete: true, Terminal: "completed", Quantities: map[string]string{"characters": "7"}})
	require.NoError(t, err)
	assert.EqualValues(t, 3, charge.FinalQuota, "a confirmed fixed speech request need not reprice incidental character counts")
}

func TestUsageSchemaRejectsUnknownPhysicalUnits(t *testing.T) {
	_, err := billing.NewQuote(billing.PriceVersion{Model: "schema", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "amount", Unit: "per_banana", Price: "0.1"}},
		UsageSchema: &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
			"amount": {Type: "number", Unit: "banana", Source: billingexpr.UsageCompletionFact, Required: true},
		}},
	}, "1", nil, time.Unix(7200, 0))
	require.ErrorContains(t, err, "unsupported billing usage unit")
}

func TestDisjointMediaTokenTotalsCannotExceedProviderAggregate(t *testing.T) {
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "multimodal", Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"},
			{Quantity: "c", Unit: "per_million_tokens", Price: "1"},
			{Quantity: "img", Unit: "per_million_tokens", Price: "1"},
			{Quantity: "ai", Unit: "per_million_tokens", Price: "1"}},
	}, "1", nil, time.Unix(7200, 0))
	require.NoError(t, err)
	for _, usage := range []billing.Usage{
		{InputTokens: 100, ImageInputTokens: 80, AudioInputTokens: 80, InputIncludesCache: true},
		{InputTokens: 100, CacheReadTokens: 20, ImageInputTokens: 80, AudioInputTokens: 80},
		{OutputTokens: 100, ImageOutputTokens: 80, AudioOutputTokens: 80},
		{OutputTokens: 100, ImageOutputTokens: 80, ReasoningTokens: 80},
	} {
		_, err := billing.Calculate(quote, usage)
		require.ErrorContains(t, err, "media token breakdown exceeds aggregate usage")
	}
	// Cache is an overlapping price category; this valid set remains accepted.
	_, err = billing.Calculate(quote, billing.Usage{InputTokens: 100, ImageInputTokens: 60, AudioInputTokens: 40, CacheReadTokens: 100, InputIncludesCache: true})
	require.NoError(t, err)
}
