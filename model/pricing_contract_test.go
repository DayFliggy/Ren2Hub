package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/require"
)

func TestPricingJSONPublishesDecimalVersionWithoutRatioFields(t *testing.T) {
	payload, err := common.Marshal(Pricing{ModelName: "priced-model",
		PriceVersion: &billing.PriceVersion{Model: "priced-model", Mode: billing.ModeUnitPrice,
			Prices: []billing.UnitPrice{{Quantity: "cc", Unit: "per_million_tokens", Price: "0.750000000000000001"}}}})
	require.NoError(t, err)
	var response map[string]interface{}
	require.NoError(t, common.Unmarshal(payload, &response))
	version := response["price_version"].(map[string]interface{})
	price := version["prices"].([]interface{})[0].(map[string]interface{})
	require.Equal(t, "0.750000000000000001", price["price"])
	for _, field := range []string{"model_ratio", "model_price", "completion_ratio", "cache_ratio", "create_cache_ratio", "image_ratio", "audio_ratio", "audio_completion_ratio"} {
		require.NotContains(t, response, field)
	}
}
