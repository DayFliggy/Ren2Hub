package plugins_test

import (
	"bytes"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

// Match the request JSON boundary used by the host, retaining fractional
// numeric source text before the plugin validates billing facts.
func exactUsageRequest(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, common.DecodeJsonNumbers(bytes.NewReader(encoded), &result))
	return result
}
