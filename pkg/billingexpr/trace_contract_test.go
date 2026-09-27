package billingexpr_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressionTracePersistsDecimalRuleAndOmitsVMResult(t *testing.T) {
	value, trace, err := billingexpr.RunExprWithRequest(`p * (attr("service_tier") == "fast" ? 0.8 : 1)`, billingexpr.TokenParams{P: 100}, billingexpr.RequestInput{Attributes: map[string]interface{}{"service_tier": "fast"}})
	require.NoError(t, err)
	assert.Equal(t, "80", value.String())
	data, err := common.Marshal(trace)
	require.NoError(t, err)
	var saved map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &saved))
	assert.NotContains(t, saved, "cost")
	rules, ok := saved["request_rules"].([]interface{})
	require.True(t, ok)
	require.Len(t, rules, 1)
	assert.Equal(t, "0.8", rules[0].(map[string]interface{})["multiplier"])
}
