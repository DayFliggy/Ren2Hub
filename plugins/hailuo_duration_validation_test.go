package plugins_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHailuoModelTiersRejectNonpositiveAndNonfiniteDurations(t *testing.T) {
	for _, model := range []string{"T2V-01", "MiniMax-Hailuo-02", "MiniMax-Hailuo-2.3"} {
		for _, duration := range []any{0, -1, nil, "bad", "Infinity"} {
			t.Run(fmt.Sprintf("%s/%v", model, duration), func(t *testing.T) {
				_, err := callBuiltinTaskHook(t, "hailuo", "buildSubmitRequest", map[string]any{
					"upstreamModel": model,
					"requestBody":   map[string]any{"prompt": "a cat", "duration": duration},
				})
				require.ErrorContains(t, err, model+" duration must be 6")
			})
		}
	}
}
