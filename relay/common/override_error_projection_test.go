package common

import (
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRetryOverrideConditionsUseOriginalErrorAfterMasking(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{Message: "specific.private.invalid rejected", Code: "provider_error"}, 502)
	err.BindSensitiveContext([]string{"https://specific.private.invalid"})
	assert.NotContains(t, err.Error(), "specific.private.invalid")
	ctx := BuildParamOverrideContext(&RelayInfo{RetryIndex: 1, LastError: err})
	operations := map[string]any{"operations": []any{map[string]any{"path": "temperature", "mode": "set", "value": 0.1, "conditions": []any{map[string]any{"path": "last_error.message", "mode": "contains", "value": "specific.private.invalid"}}}}}
	out, e := ApplyParamOverride([]byte(`{"temperature":0.7}`), operations, ctx)
	require.NoError(t, e)
	assert.JSONEq(t, `{"temperature":0.1}`, string(out))
}
