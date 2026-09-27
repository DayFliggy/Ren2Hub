package billing_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingSupportCannotInventDimensionsOrServicePolicy(t *testing.T) {
	price := billing.PriceVersion{Model: "m", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "0"}}}
	require.NoError(t, billing.ValidatePricingRequirements(price, billing.PricingRequirements{Dimensions: []string{"p"}}))
	err := billing.ValidatePricingRequirements(price, billing.PricingRequirements{Dimensions: []string{"cr"}})
	var missing *billing.PricingError
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "missing_price_dimension", missing.Code)
	price.Support = &billing.PricingSupport{Dimensions: []string{"p", "cr"}, ServiceTiers: []string{"default"}}
	require.ErrorContains(t, billing.ValidatePriceProvenance(price), "unpriced dimension")
	require.Error(t, billing.ValidatePricingRequirements(price, billing.PricingRequirements{Dimensions: []string{"cr"}}))
	price.Support = &billing.PricingSupport{Dimensions: []string{"p"}, ServiceTiers: []string{"default", "priority"}}
	require.ErrorContains(t, billing.ValidatePriceProvenance(price), "explicit expression")
	price.Support = nil
	err = billing.ValidatePricingRequirements(price, billing.PricingRequirements{ServiceTier: "priority"})
	require.ErrorAs(t, err, &missing)
	assert.Equal(t, "unverified_service_tier", missing.Code)
}

func TestPricingSupportAcceptsExplicitV2ServiceTierPolicy(t *testing.T) {
	price := billing.PriceVersion{Model: "m", Mode: billing.ModeExpression,
		Expression: `v2:attr("service_tier") == "priority" ? u("seconds") * 2 : u("seconds")`,
		UsageSchema: &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
			"seconds": {Type: "number", Unit: "seconds", Source: billingexpr.UsageCompletionFact, Required: true, Minimum: "0", Maximum: "100"},
		}}, Support: &billing.PricingSupport{Dimensions: []string{"seconds"}, ServiceTiers: []string{"default", "priority"}}}
	var err error
	price.UsageSchema, err = billingexpr.FreezeUsageSchema(price.UsageSchema)
	require.NoError(t, err)
	require.NoError(t, billing.ValidatePriceProvenance(price))
	require.NoError(t, billing.ValidatePricingRequirements(price, billing.PricingRequirements{ServiceTier: "priority", Dimensions: []string{"seconds"}}))
}
