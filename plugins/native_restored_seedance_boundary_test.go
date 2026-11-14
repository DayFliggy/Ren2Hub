//go:build known_upstream_restored

package plugins_test

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRestoredProbeSeedance25DurationBoundary(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
		_, err := plugin.Engine.Call(t.Context(), hook, map[string]any{"model": "doubao-seedance-2-5-260628", "upstreamModel": "doubao-seedance-2-5-260628", "requestBody": map[string]any{"prompt": "cat", "seconds": 31}})
		require.ErrorContains(t, err, "duration must be a positive integer no greater than 30 or -1")
	}
}
