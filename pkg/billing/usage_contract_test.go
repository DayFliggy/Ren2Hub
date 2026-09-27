package billing_test

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskPrice() billing.PriceVersion {
	return billing.PriceVersion{Model: "video", Mode: billing.ModeExpression, Expression: `v2:tier("1080p", u("seconds") * (u("resolution") == "1080p" ? 0.4 : 0.2))`, UsageSchema: &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
		"seconds":    {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact, Required: true, Maximum: "300"},
		"resolution": {Type: "string", Source: billingexpr.UsageRequestFixed, Required: true, Enum: []string{"720p", "1080p"}},
	}}}
}

func TestTaskExpressionUsesDollarsAndNeverCompletesWithEstimate(t *testing.T) {
	q, err := billing.NewQuoteWithFacts(taskPrice(), "1", nil, time.Now(), map[string]interface{}{"resolution": "1080p"})
	require.NoError(t, err)
	u := billing.Usage{EstimatedFacts: map[string]interface{}{"seconds": "30"}, ActualFacts: map[string]interface{}{"seconds": "10"}, Complete: true}
	result, err := billing.Calculate(q, u)
	require.NoError(t, err)
	assert.Equal(t, "4", result.TotalUSD)
	assert.Equal(t, common.Quota(2000000), result.FinalQuota)
	assert.Equal(t, "1080p", result.Trace.MatchedTier)
	u.ActualFacts = nil
	_, err = billing.Calculate(q, u)
	require.ErrorContains(t, err, "missing")
	u.ActualFacts = map[string]interface{}{"seconds": "0"}
	result, err = billing.Calculate(q, u)
	require.NoError(t, err)
	assert.Zero(t, result.FinalQuota)
	u.Estimated = true
	u.Complete = false
	result, err = billing.Calculate(q, u)
	require.NoError(t, err)
	assert.Equal(t, "12", result.TotalUSD)
}

func TestUsageSchemaIsFrozenAndValidatedAcrossAllBranches(t *testing.T) {
	price := taskPrice()
	q, err := billing.NewQuoteWithFacts(price, "1", nil, time.Now(), map[string]interface{}{"resolution": "1080p"})
	require.NoError(t, err)
	price.UsageSchema.Fields["resolution"] = billingexpr.UsageField{Type: "boolean"}
	_, err = billing.Calculate(q, billing.Usage{Complete: true, ActualFacts: map[string]interface{}{"seconds": "10"}})
	require.NoError(t, err, "mutating a draft cannot mutate a frozen quote")
	for _, expression := range []string{`v2:false ? u("missing") : 0`, `v2:u("sec" + "onds")`, `v2:fixed(1)`, `v2:p * 2`, `v3:1`, `v2:[u][0]("seconds")`} {
		p := taskPrice()
		p.Expression = expression
		_, err := billing.NewQuoteWithFacts(p, "1", nil, time.Now(), map[string]interface{}{"resolution": "1080p"})
		require.Error(t, err, expression)
	}
	changed := taskPrice()
	delete(changed.UsageSchema.Fields, "seconds")
	_, err = billing.NewQuoteWithFacts(changed, "1", nil, time.Now(), map[string]interface{}{"resolution": "1080p"})
	require.Error(t, err, "cached compilation cannot bypass a different schema")
}

func TestUsageFactsRejectWrongSourceAndDuplicateFees(t *testing.T) {
	q, err := billing.NewQuoteWithFacts(taskPrice(), "1", nil, time.Now(), map[string]interface{}{"resolution": "1080p"})
	require.NoError(t, err)
	for _, facts := range []map[string]interface{}{{"seconds": nil}, {"seconds": "-1"}, {"seconds": "301"}, {"seconds": "10", "resolution": "720p"}, {"seconds": "10", "unknown": 1}} {
		_, err := billing.Calculate(q, billing.Usage{Complete: true, ActualFacts: facts})
		require.Error(t, err)
	}
	price := taskPrice()
	price.Prices = []billing.UnitPrice{{Quantity: "seconds", Unit: "per_second", Price: "0.4"}}
	_, err = billing.NewQuoteWithFacts(price, "1", nil, time.Now(), map[string]interface{}{"resolution": "1080p"})
	require.ErrorContains(t, err, "already owns")
}

func TestBillingContractsRejectOldSnapshotsAndWrongUnits(t *testing.T) {
	q, err := billing.NewQuote(billing.PriceVersion{Model: "fixed", Mode: billing.ModeExpression, Expression: `v1:fixed(4)`}, "1", nil, time.Now())
	require.NoError(t, err)
	result, err := billing.Calculate(q, billing.Usage{})
	require.NoError(t, err)
	assert.Equal(t, common.Quota(2000000), result.FinalQuota)
	q.ContractVersion = 1
	q.Hash = ""
	q.Hash, err = billing.Hash(q)
	require.NoError(t, err)
	_, err = billing.Calculate(q, billing.Usage{})
	require.ErrorContains(t, err, "contract")
	p := taskPrice()
	p.ResultUnit = billing.ResultMicroUSD
	_, err = billing.NewQuoteWithFacts(p, "1", nil, time.Now(), map[string]interface{}{"resolution": "1080p"})
	require.ErrorContains(t, err, "unit")
}

func TestSchemaUnitPricesUseFactsEvenWhenNamesMatchTokenDimensions(t *testing.T) {
	for _, field := range []string{"p", "c", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao"} {
		t.Run(field, func(t *testing.T) {
			price := billing.PriceVersion{Model: "plugin-meter", Mode: billing.ModeUnitPrice,
				Prices: []billing.UnitPrice{{Quantity: field, Unit: "per_second", Price: "0.4"}},
				UsageSchema: &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
					field: {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact, Required: true},
				}}}
			quote, err := billing.NewQuote(price, "1", nil, time.Now())
			require.NoError(t, err)
			for _, tokens := range []int64{0, 100} {
				usage := billing.Usage{Complete: true, ActualFacts: map[string]any{field: "10"},
					InputTokens: tokens * 2, OutputTokens: tokens * 2, CacheReadTokens: tokens, CacheWriteTokens: tokens,
					CacheWrite1hTokens: tokens, ImageInputTokens: tokens, ImageOutputTokens: tokens,
					AudioInputTokens: tokens, AudioOutputTokens: tokens}
				result, err := billing.Calculate(quote, usage)
				require.NoError(t, err)
				assert.Equal(t, "4", result.TotalUSD, "declared seconds must not become legacy token counts")
				assert.EqualValues(t, 2000000, result.FinalQuota)
				require.Len(t, result.Lines, 1)
				assert.Equal(t, "10", result.Lines[0].Count)
			}
		})
	}
}
