package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCatalogPriceUsesNativeUnitsAndOneChannelFactor(t *testing.T) {
	previous := common.QuotaPerUnit
	common.QuotaPerUnit = 500000
	t.Cleanup(func() { common.QuotaPerUnit = previous })
	base := catalogPrice(PricingValues{"ModelRatio": float64(1.5), "CompletionRatio": float64(4)})
	require.Equal(t, map[string]string{"input": "3", "output": "12"}, base.PricingValues)
	scaled, err := ScaleCatalogPrice(base, "1.25")
	require.NoError(t, err)
	require.Equal(t, map[string]string{"input": "3.75", "output": "15"}, scaled.PricingValues)
	require.Equal(t, "3", base.PricingValues["input"], "projection must not mutate base prices")
	zero := catalogPrice(PricingValues{"ModelPrice": float64(0)})
	require.Equal(t, "zero", zero.PriceStatus)
	require.Equal(t, "0", zero.PricingValues["request"])
	require.Equal(t, "unavailable", catalogPrice(PricingValues{}).PriceStatus)
	expr := catalogPrice(PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `tier("固定", p * 1.234567890123456789 + c * 0)`})
	require.Equal(t, "1.234567890123456789", expr.PricingValues["input"])
	dynamic := catalogPrice(PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `p * param("rate")`})
	require.Equal(t, "unavailable", dynamic.PriceStatus)
	require.Empty(t, dynamic.PricingValues)
}
