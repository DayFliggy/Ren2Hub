package dto

import (
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGeminiThinkingPreservesExplicitFalseAndZero(t *testing.T) {
	for _, input := range []string{`{"includeThoughts":false,"thinkingBudget":0}`, `{"include_thoughts":false,"thinking_budget":0}`} {
		var value GeminiThinkingConfig
		require.NoError(t, kitutil.Unmarshal([]byte(input), &value))
		require.NotNil(t, value.IncludeThoughts)
		assert.False(t, *value.IncludeThoughts)
		require.NotNil(t, value.ThinkingBudget)
		assert.Zero(t, *value.ThinkingBudget)
		encoded, err := kitutil.Marshal(value)
		require.NoError(t, err)
		assert.JSONEq(t, `{"includeThoughts":false,"thinkingBudget":0}`, string(encoded))
	}
	var absent GeminiThinkingConfig
	require.NoError(t, kitutil.Unmarshal([]byte(`{}`), &absent))
	assert.Nil(t, absent.IncludeThoughts)
	assert.Nil(t, absent.ThinkingBudget)
}
