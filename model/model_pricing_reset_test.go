package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestModelPricingResetRejectsEntireBatchBeforeDatabaseAccess(t *testing.T) {
	previousDB := DB
	DB = nil
	t.Cleanup(func() { DB = previousDB })
	for _, raw := range []string{
		`[{"model_name":"reset-test","reset":true}]`,
		`[{"model_name":"save-test","expected_version":"version","pricing":{"ModelPrice":3}},{"model_name":"reset-test","reset":true}]`,
	} {
		var changes []ModelPricingChange
		require.NoError(t, common.Unmarshal([]byte(raw), &changes))
		require.EqualError(t, UpdateModelPricing(changes), "no built-in prices to restore; submit the prices to save")
	}
}
