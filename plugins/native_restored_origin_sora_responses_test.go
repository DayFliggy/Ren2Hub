//go:build known_upstream_restored

package plugins_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoredProbeSoraSubmitSecondsMatchNumericUsage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request map[string]any
		want    string
		usage   float64
	}{
		{"default", map[string]any{}, "4", 4},
		{"numeric", map[string]any{"seconds": 8}, "8", 8},
		{"string", map[string]any{"seconds": "12"}, "12", 12},
		{"alias", map[string]any{"duration": 4}, "4", 4},
		{"matching aliases", map[string]any{"seconds": "8", "duration": 8}, "8", 8},
	} {
		for _, multipart := range []bool{false, true} {
			name := tc.name + "/json"
			if multipart {
				name = tc.name + "/multipart"
			}
			t.Run(name, func(t *testing.T) {
				request := map[string]any{"prompt": "waves at sunset"}
				for key, value := range tc.request {
					request[key] = value
				}
				ctx := map[string]any{"upstreamModel": "sora-2-pro", "requestBody": request}
				if multipart {
					ctx["files"] = []any{map[string]any{"field": "input_reference", "ref": "file-0", "filename": "frame.png"}}
				}
				submitted, err := restoredProbeBuiltinHook(t, "sora", "buildSubmitRequest", ctx)
				require.NoError(t, err)
				if multipart {
					values := map[string]any{}
					for _, raw := range submitted["parts"].([]any) {
						part := raw.(map[string]any)
						values[part["name"].(string)] = part["value"]
					}
					assert.Equal(t, tc.want, values["seconds"])
					assert.NotContains(t, values, "duration")
				} else {
					body := submitted["body"].(map[string]any)
					assert.Equal(t, tc.want, body["seconds"])
					assert.NotContains(t, body, "duration")
				}
				facts, err := restoredProbeBuiltinHook(t, "sora", "extractUsage", ctx)
				require.NoError(t, err)
				assert.Equal(t, tc.usage, facts["seconds"])
				assert.Equal(t, "720x1280", facts["size"])
			})
		}
	}
}

func TestRestoredProbeSoraRejectsInvalidSecondsBeforeSubmissionAndBilling(t *testing.T) {
	for _, value := range []any{0, -4, 5, 4.5, 3601, "", "bad", nil, true, []any{4}, map[string]any{}} {
		for _, key := range []string{"seconds", "duration"} {
			ctx := map[string]any{"upstreamModel": "sora-2", "requestBody": map[string]any{"prompt": "p", key: value}}
			for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
				_, err := restoredProbeBuiltinHook(t, "sora", hook, ctx)
				require.ErrorContains(t, err, "seconds must be one of 4, 8, or 12", "%s %s=%#v", hook, key, value)
			}
		}
	}
	ctx := map[string]any{"upstreamModel": "sora-2", "requestBody": map[string]any{"prompt": "p", "seconds": 4, "duration": 8}}
	_, err := restoredProbeBuiltinHook(t, "sora", "buildSubmitRequest", ctx)
	require.ErrorContains(t, err, "seconds and duration must match")
}
