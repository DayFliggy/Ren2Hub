package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestPricingJSONPublishesNewAPIModelConfiguration(t *testing.T) {
	cache := 0.25
	payload, err := common.Marshal(Pricing{ModelName: "priced-model", ModelRatio: 1.5, CompletionRatio: 2, CacheRatio: &cache, PricingVersion: "configuration-hash", BillingMode: "ratio"})
	require.NoError(t, err)
	var response map[string]interface{}
	require.NoError(t, common.Unmarshal(payload, &response))
	require.Equal(t, 1.5, response["model_ratio"])
	require.Equal(t, 2.0, response["completion_ratio"])
	require.Equal(t, 0.25, response["cache_ratio"])
	require.Equal(t, "configuration-hash", response["pricing_version"])
	for _, field := range []string{"price_version", "price_versions", "offers", "provenance", "channel_id"} {
		require.NotContains(t, response, field)
	}
}
