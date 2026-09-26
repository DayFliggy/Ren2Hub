package billing_test

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmptyUsageScopePreservesPublishedWireContractAndQuoteHash(t *testing.T) {
	const legacyPrice = `{"contract_version":2,"calculation_version":"billing-v2","result_unit":"usd","id":0,"model":"embedding","mode":"unit_price","prices":[{"quantity":"p","unit":"per_million_tokens","price":"0.1"}],"created_at":0,"hash":""}`
	var price billing.PriceVersion
	require.NoError(t, common.Unmarshal([]byte(legacyPrice), &price))
	prepared, err := billing.PreparePriceVersion(price)
	require.NoError(t, err)
	encoded, err := common.Marshal(prepared)
	require.NoError(t, err)
	assert.Equal(t, legacyPrice, string(encoded))
	hash, err := billing.Hash(prepared)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(legacyPrice))), hash)

	quote, err := billing.NewQuote(prepared, "1", nil, time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	legacyQuote := `{"contract_version":2,"calculation_version":"billing-v2","price_version":` + legacyPrice + `,"channel_ratio":"1","frozen_at":"2026-09-26T00:00:00Z","hash":"","violation_fee_enabled":false,"violation_fee_usd":""}`
	assert.Equal(t, fmt.Sprintf("%x", sha256.Sum256([]byte(legacyQuote))), quote.Hash)
}

func TestEmbeddingScopeValidatesOnlyInputTokenPrices(t *testing.T) {
	for _, price := range []billing.PriceVersion{
		{Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "0.1"}, {Quantity: "ai", Unit: "per_million_tokens", Price: "1"}}},
		{Mode: billing.ModeExpression, Expression: `v1:len > 100 ? p * 2 + cr * 0.1 : p + cr * 0.1`},
		{Mode: billing.ModeExpression, Expression: `v1:p * 0`},
		{Mode: billing.ModeFree},
	} {
		price.Model, price.UsageScope = "embedding", billing.UsageScopeEmbedding
		prepared, err := billing.PreparePriceVersion(price)
		require.NoError(t, err)
		require.NoError(t, billing.ValidateFrozenPriceVersion(prepared))
	}

	cases := []struct {
		name  string
		price billing.PriceVersion
	}{
		{"unknown_scope", billing.PriceVersion{Mode: billing.ModeFree, UsageScope: "rerank"}},
		{"task_schema", taskPrice()},
		{"v2", billing.PriceVersion{Mode: billing.ModeExpression, Expression: `v2:0`}},
		{"output", billing.PriceVersion{Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}, {Quantity: "c", Unit: "per_million_tokens", Price: "0"}}}},
		{"missing_input", billing.PriceVersion{Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "ai", Unit: "per_million_tokens", Price: "1"}}}},
		{"tool", billing.PriceVersion{Mode: billing.ModeExpression, Expression: `p`, Prices: []billing.UnitPrice{{Quantity: "tool:web_search", Unit: "per_call", Price: "0.01"}}}},
		{"image", billing.PriceVersion{Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "images", Unit: "per_image", Price: "0.01"}}}},
	}
	for _, expression := range []string{`c`, `p + img_o`, `p + ao`, `p + fixed(0.01)`, `p + image_count`, `false ? c : p`, `false ? fixed(0.01) : p`, `false ? image_count : p`} {
		cases = append(cases, struct {
			name  string
			price billing.PriceVersion
		}{expression, billing.PriceVersion{Mode: billing.ModeExpression, Expression: "v1:" + expression}})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			price := test.price
			price.Model = "embedding"
			if price.UsageScope == "" {
				price.UsageScope = billing.UsageScopeEmbedding
			}
			_, err := billing.PreparePriceVersion(price)
			require.Error(t, err)
		})
	}
}

func TestEmbeddingScopeIsFrozenAndRejectsEveryOutputRepresentation(t *testing.T) {
	for _, mode := range []string{billing.ModeExpression, billing.ModeUnitPrice, billing.ModeFree} {
		t.Run(mode, func(t *testing.T) {
			price := billing.PriceVersion{Model: "embedding", Mode: mode, UsageScope: billing.UsageScopeEmbedding}
			if mode == billing.ModeExpression {
				price.Expression = `v1:p * 0.1`
			} else if mode == billing.ModeUnitPrice {
				price.Prices = []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "0.1"}}
			}
			quote, err := billing.NewQuote(price, "1", nil, time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC))
			require.NoError(t, err)
			price.UsageScope = ""
			assert.Equal(t, billing.UsageScopeEmbedding, quote.PriceVersion.UsageScope)
			_, err = billing.Calculate(quote, billing.Usage{InputTokens: 100, InputIncludesCache: true, Complete: true})
			require.NoError(t, err)
			for _, usage := range []billing.Usage{
				{OutputTokens: 1}, {ImageOutputTokens: 1}, {AudioOutputTokens: 1},
				{Quantities: map[string]string{"images": "1"}},
				{Quantities: map[string]string{"calls": "1"}},
				{Quantities: map[string]string{"tool:web_search": "1"}},
			} {
				usage.Complete = true
				_, err := billing.Calculate(quote, usage)
				require.ErrorContains(t, err, "embedding usage")
			}
			quote.PriceVersion.UsageScope = ""
			_, err = billing.Calculate(quote, billing.Usage{})
			require.ErrorContains(t, err, "integrity")
		})
	}
}

func TestUnscopedManualExpressionKeepsItsPricingPolicy(t *testing.T) {
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "manual", Mode: billing.ModeExpression, Expression: `v1:p * 0.1`}, "1", map[string]interface{}{"service_tier": "priority"}, time.Now())
	require.NoError(t, err)
	result, err := billing.Calculate(quote, billing.Usage{InputTokens: 100, OutputTokens: 50, InputIncludesCache: true})
	require.NoError(t, err)
	assert.Equal(t, "0.00001", result.TotalUSD, "manual expressions do not inherit automatic tier multipliers or embedding scope")
}
