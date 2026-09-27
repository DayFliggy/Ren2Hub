package billing_test

import (
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestImageCountExpressionRequiresBoundedActualQuantity(t *testing.T) {
	version := billing.PriceVersion{Model: "image", Mode: billing.ModeExpression,
		Expression: `v1:tier("images", fixed(0.0000006)) * image_count`,
		Prices:     []billing.UnitPrice{{Quantity: "tool:search", Unit: "per_call", Price: "0.0000006"}}}
	q, err := billing.NewQuote(version, "1", map[string]any{"quantity": "3"}, time.Now())
	require.NoError(t, err)
	for _, count := range []string{"", "0", "-1", "1.5", "129", "NaN", "9007199254740993"} {
		u := billing.Usage{Quantities: map[string]string{"images": count}, Complete: true}
		if count == "" {
			delete(u.Quantities, "images")
		}
		_, err := billing.Calculate(q, u)
		require.Error(t, err, count)
	}
	charge, err := billing.Calculate(q, billing.Usage{Quantities: map[string]string{"images": "2", "tool:search": "1"}, Complete: true})
	require.NoError(t, err)
	assert.Equal(t, "0.0000018", charge.TotalUSD)
	assert.EqualValues(t, 1, charge.FinalQuota, "round all dimensions only once")
	require.NotNil(t, charge.Trace.ImageCount)
	assert.Equal(t, 2, *charge.Trace.ImageCount)
	assert.Equal(t, "3", q.Attributes["quantity"], "actual count cannot replace the frozen request")
	for _, count := range []string{"1", "128"} {
		_, err := billing.Calculate(q, billing.Usage{Quantities: map[string]string{"images": count, "tool:search": "0"}, Complete: true})
		require.NoError(t, err, count)
	}
	for _, expression := range []string{version.Expression, `v1:true ? fixed(0.1) : image_count * fixed(1)`} {
		_, err := billing.PreparePriceVersion(billing.PriceVersion{Model: "image", Mode: billing.ModeExpression, Expression: expression,
			Prices: []billing.UnitPrice{{Quantity: "images", Unit: "per_image", Price: "0"}}})
		require.ErrorContains(t, err, "already owns image quantity")
	}
	_, _, err = billingexpr.RunExpr(version.Expression, billingexpr.TokenParams{})
	require.ErrorContains(t, err, "image_count")
	schema, err := billingexpr.FreezeUsageSchema(&billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
		"count": {Type: "number", Unit: "image", Source: billingexpr.UsageCompletionFact, Required: true},
	}})
	require.NoError(t, err)
	_, err = billingexpr.CompileWithUsageSchema(`v2:image_count * 0.1`, schema)
	require.Error(t, err, "v2 cannot borrow the v1 image variable")
}

func TestImageCacheIsCountedOnceAcrossTokenDimensions(t *testing.T) {
	q, err := billing.NewQuote(billing.PriceVersion{Model: "image", Mode: billing.ModeExpression,
		Expression: `p * 5 + cr * 1.25 + img * 8 + img_cr * 2 + c * 30`}, "1", nil, time.Now())
	require.NoError(t, err)
	cached := int64(200)
	usage := billing.Usage{InputTokens: 1000, OutputTokens: 100, ContextTokens: 1000, ImageInputTokens: 600, CacheReadTokens: 300, ImageCacheReadTokens: &cached, InputIncludesCache: true, Complete: true}
	charge, err := billing.Calculate(q, usage)
	require.NoError(t, err)
	assert.Equal(t, "0.008225", charge.TotalUSD)
	assert.EqualValues(t, 4113, charge.FinalQuota)
	// Absent detail follows the upstream aggregate-cache contract; it does not
	// silently infer the unknown image fraction from the known total cache.
	usage.ImageCacheReadTokens = nil
	charge, err = billing.Calculate(q, usage)
	require.NoError(t, err)
	assert.Equal(t, "0.008675", charge.TotalUSD)
	cached = 0
	usage.ImageCacheReadTokens = &cached
	charge, err = billing.Calculate(q, usage)
	require.NoError(t, err)
	assert.Equal(t, "0.008675", charge.TotalUSD)
	for _, invalid := range []int64{-1, 301, 1001} {
		usage.ImageCacheReadTokens = &invalid
		_, err = billing.Calculate(q, usage)
		require.ErrorContains(t, err, "image cache")
	}
}
