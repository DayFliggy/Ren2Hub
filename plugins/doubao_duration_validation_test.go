package plugins_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDoubaoHostDurationNumberAndStringSurviveDecoding(t *testing.T) {
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

func TestDoubaoResponsesNullSecondsUsesOnlyRealDecodedSeconds(t *testing.T) {
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

func TestDoubaoVendorBodyDurationTypesRemainNative(t *testing.T) {
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
