package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestUserBillingLogProjectionKeepsExactAdjustmentAndRejectsMalformedMultiplier(t *testing.T) {
	charge := billing.ChargeResult{CalculationVersion: billing.CalculationVersion, BaseUSD: "0.123456789012345678", TotalUSD: "0.061728394506172839", FinalQuota: 1,
		Adjustments: []billing.ChargeAdjustment{{Kind: "channel_multiplier", Multiplier: "0.5", BaseUSD: "0.123456789012345678", AmountUSD: "-0.061728394506172839", ResultUSD: "0.061728394506172839"}}}
	raw, err := common.Marshal(map[string]any{"charge_result": charge, "channel_multiplier": "0.500000000000000000", "requested_service_tier": "priority", "actual_service_tier": "default", "secret": "must not project"})
	require.NoError(t, err)
	result := userBillingLogMetadata(string(raw))
	require.Equal(t, charge, result["charge_result"])
	require.Equal(t, "0.5", result["channel_multiplier"])
	require.Equal(t, "priority", result["requested_service_tier"])
	require.Equal(t, "default", result["actual_service_tier"])
	require.NotContains(t, result, "secret")
	for _, value := range []any{"NaN", "-1", "0", 1.5, "1001"} {
		raw, err = common.Marshal(map[string]any{"channel_multiplier": value})
		require.NoError(t, err)
		require.NotContains(t, userBillingLogMetadata(string(raw)), "channel_multiplier")
	}
}
