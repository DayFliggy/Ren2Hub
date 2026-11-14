//go:build known_upstream_restored

package plugins_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoredProbeDoubaoResponsesDurationFallbackValidation(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	const model = "doubao-seedance-1-5-pro-251215"
	for _, field := range []string{"seconds", "duration"} {
		for _, empty := range []struct {
			name  string
			value any
		}{{"null", nil}, {"empty", ""}, {"zero", 0}} {
			for _, tc := range []struct {
				name     string
				metadata map[string]any
				message  string
			}{
				{"above maximum", map[string]any{"duration": 999}, "duration must be a positive integer no greater than 12 or -1"},
				{"conflicting frames", map[string]any{"duration": 12, "frames": 48}, "frames and duration must describe the same supported output length"},
			} {
				t.Run(field+"/"+empty.name+"/"+tc.name, func(t *testing.T) {
					decoded, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_responses", "decodeRequest"}, map[string]any{
						"model": model,
						"body": map[string]any{"kind": "json", "value": map[string]any{
							"model": model, "input": "a cat", field: empty.value, "metadata": tc.metadata,
						}},
					})
					require.NoError(t, err)
					ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": alibabaObject(t, decoded)["requestBody"]}
					for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
						t.Run(hook, func(t *testing.T) {
							_, err := plugin.Engine.Call(t.Context(), hook, ctx)
							require.ErrorContains(t, err, tc.message)
						})
					}
				})
			}
		}
	}
}

func TestRestoredProbeDoubaoDurationSelectionMatchesSubmissionAndReservation(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	const model = "doubao-seedance-1-5-pro-251215"
	for _, tc := range []struct {
		name         string
		request      map[string]any
		wantDuration any
		wantFrames   any
		wantSeconds  float64
	}{
		{"seconds", map[string]any{"seconds": 12}, float64(12), nil, 12},
		{"duration alias", map[string]any{"duration": 12}, float64(12), nil, 12},
		{"empty seconds uses metadata", map[string]any{"seconds": "", "metadata": map[string]any{"duration": 12}}, float64(12), nil, 12},
		{"zero seconds uses metadata", map[string]any{"seconds": 0, "metadata": map[string]any{"duration": 12}}, float64(12), nil, 12},
		{"null seconds uses metadata", map[string]any{"seconds": nil, "metadata": map[string]any{"duration": 12}}, float64(12), nil, 12},
		{"seconds automatic", map[string]any{"seconds": -1}, float64(-1), nil, 12},
		{"automatic overrides metadata", map[string]any{"seconds": -1, "metadata": map[string]any{"duration": 6}}, float64(-1), nil, 12},
		{"metadata automatic", map[string]any{"seconds": nil, "metadata": map[string]any{"duration": -1}}, float64(-1), nil, 12},
		{"frames only", map[string]any{"metadata": map[string]any{"frames": 48}}, nil, float64(48), 2},
		{"empty seconds with frames only", map[string]any{"seconds": "", "metadata": map[string]any{"frames": 48}}, nil, float64(48), 2},
		{"matching frames", map[string]any{"seconds": nil, "metadata": map[string]any{"duration": 12, "frames": 288}}, float64(12), float64(288), 12},
		{"omitted duration reserves maximum", map[string]any{}, nil, nil, 12},
		{"zero seconds reserves maximum", map[string]any{"seconds": 0}, nil, nil, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": tc.request}
			submitted, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			require.NoError(t, err)
			body := alibabaObject(t, submitted)["body"].(map[string]any)
			assert.Equal(t, tc.wantDuration, body["duration"])
			assert.Equal(t, tc.wantFrames, body["frames"])
			if tc.wantDuration == nil {
				assert.NotContains(t, body, "duration")
			}
			usage, err := plugin.Engine.Call(t.Context(), "extractUsage", ctx)
			require.NoError(t, err)
			assert.Equal(t, tc.wantSeconds*1920*1080*24/1024, alibabaObject(t, usage)["tokens"])
		})
	}
	for _, request := range []map[string]any{
		{"seconds": -1, "metadata": map[string]any{"frames": 48}},
		{"seconds": nil, "metadata": map[string]any{"duration": -1, "frames": 48}},
	} {
		ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": request}
		for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
			_, err := plugin.Engine.Call(t.Context(), hook, ctx)
			require.ErrorContains(t, err, "frames cannot be combined with automatic duration")
		}
	}
	for _, request := range []map[string]any{
		{"seconds": "0", "metadata": map[string]any{"duration": 12}},
		{"metadata": map[string]any{"duration": 0}},
	} {
		ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": request}
		for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
			_, err := plugin.Engine.Call(t.Context(), hook, ctx)
			require.ErrorContains(t, err, "duration must be a positive integer no greater than 12 or -1")
		}
	}
}

func TestRestoredProbeDoubaoHostNonScalarDurationUsesMaximumAfterRealDecoding(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	const model = "doubao-seedance-1-5-pro-251215"
	for _, protocol := range []string{"openai_responses", "openai_video"} {
		for _, tc := range []struct {
			name   string
			fields map[string]any
		}{
			{"boolean", map[string]any{"seconds": true}},
			{"false with frames", map[string]any{"seconds": false, "metadata": map[string]any{"frames": 48}}},
			{"null", map[string]any{"seconds": nil}},
			{"null with frames", map[string]any{"seconds": nil, "metadata": map[string]any{"frames": 48}}},
			{"null alias with frames", map[string]any{"duration": nil, "metadata": map[string]any{"frames": 48}}},
			{"metadata null with frames", map[string]any{"metadata": map[string]any{"duration": nil, "frames": 48}}},
			{"array", map[string]any{"seconds": []any{6}}},
			{"object", map[string]any{"seconds": map[string]any{}}},
			{"boolean with frames", map[string]any{"seconds": true, "metadata": map[string]any{"frames": 48}}},
			{"metadata array with frames", map[string]any{"metadata": map[string]any{"duration": []any{6}, "frames": 48}}},
		} {
			t.Run(protocol+"/"+tc.name, func(t *testing.T) {
				rawRequest := map[string]any{"model": model, "input": "a cat", "prompt": "a cat"}
				for key, value := range tc.fields {
					rawRequest[key] = value
				}
				decoded, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{protocol, "decodeRequest"}, map[string]any{
					"model": model,
					"body":  map[string]any{"kind": "json", "value": rawRequest},
				})
				if protocol == "openai_video" && (tc.name == "object" || tc.name == "false with frames" || tc.name == "null" || tc.name == "null with frames" || tc.name == "null alias with frames") {
					// Preserve the upstream decoder's existing Number() range check.
					require.ErrorContains(t, err, "seconds must be -1 or between 1 and 3600")
					return
				}
				require.NoError(t, err)
				ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": decodePluginValue(t, decoded)["requestBody"]}
				submitted, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
				require.NoError(t, err)
				body := decodePluginValue(t, submitted)["body"].(map[string]any)
				assert.NotContains(t, body, "duration")
				usage, err := plugin.Engine.Call(t.Context(), "extractUsage", ctx)
				require.NoError(t, err)
				assert.Equal(t, float64(12*1920*1080*24/1024), decodePluginValue(t, usage)["tokens"])
			})
		}
	}
}

func TestRestoredProbeDoubaoHostDurationNumberAndStringSurviveDecoding(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	const model = "doubao-seedance-1-5-pro-251215"
	for _, protocol := range []string{"openai_responses", "openai_video"} {
		for _, value := range []any{float64(6), "6"} {
			t.Run(fmt.Sprintf("%s/%T", protocol, value), func(t *testing.T) {
				request := map[string]any{"model": model, "input": "a cat", "prompt": "a cat", "seconds": value}
				decoded, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{protocol, "decodeRequest"}, map[string]any{
					"model": model,
					"body":  map[string]any{"kind": "json", "value": request},
				})
				require.NoError(t, err)
				decodedRequest := decodePluginValue(t, decoded)["requestBody"].(map[string]any)
				assert.IsType(t, value, decodedRequest["seconds"])
				ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": decodedRequest}
				submitted, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
				require.NoError(t, err)
				assert.Equal(t, float64(6), decodePluginValue(t, submitted)["body"].(map[string]any)["duration"])
				usage, err := plugin.Engine.Call(t.Context(), "extractUsage", ctx)
				require.NoError(t, err)
				assert.Equal(t, float64(6*1920*1080*24/1024), decodePluginValue(t, usage)["tokens"])
			})
		}
	}
}

func TestRestoredProbeDoubaoResponsesNullSecondsUsesOnlyRealDecodedSeconds(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	const model = "doubao-seedance-1-5-pro-251215"
	decoded, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_responses", "decodeRequest"}, map[string]any{
		"model": model,
		"body": map[string]any{"kind": "json", "value": map[string]any{
			"model": model, "input": "a cat", "seconds": nil, "duration": 6,
		}},
	})
	require.NoError(t, err)
	request := decodePluginValue(t, decoded)["requestBody"].(map[string]any)
	assert.Contains(t, request, "seconds")
	assert.Nil(t, request["seconds"])
	assert.NotContains(t, request, "duration")
	ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": request}
	submitted, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
	require.NoError(t, err)
	body := decodePluginValue(t, submitted)["body"].(map[string]any)
	assert.NotContains(t, body, "duration")
	usage, err := plugin.Engine.Call(t.Context(), "extractUsage", ctx)
	require.NoError(t, err)
	assert.Equal(t, float64(12*1920*1080*24/1024), decodePluginValue(t, usage)["tokens"])
}

func TestRestoredProbeDoubaoVendorBodyDurationTypesRemainNative(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	const model = "doubao-seedance-1-5-pro-251215"
	for _, value := range []any{true, []any{6}, map[string]any{}} {
		ctx := map[string]any{"model": model, "upstreamModel": model, "requestBody": map[string]any{
			"vendorBody": map[string]any{"model": model, "duration": value, "content": []any{}}}}
		submitted, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
		require.NoError(t, err)
		assert.IsType(t, value, decodePluginValue(t, submitted)["body"].(map[string]any)["duration"])
		_, err = plugin.Engine.Call(t.Context(), "extractUsage", ctx)
		require.NoError(t, err)
	}
}
