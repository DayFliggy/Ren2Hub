package billing_test

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressionSettlementUsesActualContextTierWithFrozenAdjustment(t *testing.T) {
	version := billing.PriceVersion{Model: "context-tiers", Mode: billing.ModeExpression,
		Expression: `len <= 100000 ? tier("small", p + cr * 0.1) : tier("large", p * 2 + cr * 0.2)`}
	quote, err := billing.NewQuote(version, "1.5", nil, time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	for _, test := range []struct {
		name  string
		input int64
		tier  string
		usd   string
		quota common.Quota
	}{
		{"at threshold", 100000, "small", "0.0285", 14250},
		{"crossing threshold", 100001, "large", "0.057003", 28502},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := billing.Calculate(quote, billing.Usage{InputTokens: test.input, ContextTokens: test.input, CacheReadTokens: 90000, InputIncludesCache: true, Complete: true})
			require.NoError(t, err)
			require.NotNil(t, result.Trace)
			assert.Equal(t, test.tier, result.Trace.MatchedTier)
			assert.Equal(t, test.usd, result.TotalUSD)
			assert.Equal(t, test.quota, result.FinalQuota)
		})
	}
}
