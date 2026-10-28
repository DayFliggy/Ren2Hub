package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func callBuiltinTaskHook(t *testing.T, key, hook string, ctx map[string]any) (map[string]any, error) {
	t.Helper()
	source, err := builtinplugins.Source(key)
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: key})
	require.NoError(t, err)
	value, err := plugin.Engine.Call(t.Context(), hook, ctx)
	if err != nil {
		return nil, err
	}
	return decodePluginValue(t, value), nil
}

func TestKlingSubmitUsesBilledModeAndModelAfterMetadataMerge(t *testing.T) {
	ctx := map[string]any{
		"apiKey":        "test-key",
		"upstreamModel": "kling-v1",
		"requestBody": map[string]any{
			"model": "kling-v1", "prompt": "a cat", "mode": "std",
			"metadata": map[string]any{"model": "kling-v2-master", "model_name": "kling-v2-master", "mode": "pro"},
		},
	}
	submitted, err := callBuiltinTaskHook(t, "kling", "buildSubmitRequest", ctx)
	require.NoError(t, err)
	body := submitted["body"].(map[string]any)
	assert.Equal(t, "kling-v1", body["model"])
	assert.Equal(t, "kling-v1", body["model_name"])
	assert.Equal(t, "std", body["mode"])
	facts, err := callBuiltinTaskHook(t, "kling", "extractUsage", ctx)
	require.NoError(t, err)
	assert.Equal(t, 1.0, facts["units"])
}

func TestVeoSubmitValidatesFinalBillingParameters(t *testing.T) {
	for _, tc := range []struct {
		key     string
		message string
		ctx     map[string]any
	}{
		{"google", "durationSeconds must be one of 4, 6, or 8", map[string]any{
			"upstreamModel": "veo-3.1-fast-generate-preview", "baseUrl": "https://example.test", "apiKey": "key",
			"requestBody": map[string]any{"prompt": "a cat", "metadata": map[string]any{"durationSeconds": 7}},
		}},
		{"vertex-ai", "durationSeconds must be one of 4, 6, or 8", map[string]any{
			"upstreamModel": "veo-3.1-fast-generate-preview", "baseUrl": "https://example.test", "authHeader": "Bearer key",
			"auth":        map[string]any{"projectId": "p"},
			"requestBody": map[string]any{"prompt": "a cat", "metadata": map[string]any{"durationSeconds": 7}},
		}},
		{"vertex-ai", "generateAudio must be a boolean", map[string]any{
			"upstreamModel": "veo-3.1-fast-generate-preview", "baseUrl": "https://example.test", "authHeader": "Bearer key",
			"auth":        map[string]any{"projectId": "p"},
			"requestBody": map[string]any{"prompt": "a cat", "metadata": map[string]any{"generateAudio": "false"}},
		}},
	} {
		t.Run(tc.key+"/"+tc.message, func(t *testing.T) {
			_, err := callBuiltinTaskHook(t, tc.key, "buildSubmitRequest", tc.ctx)
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestTaskPluginsRejectBillingInputsThatWouldBeRewrittenOrUnderstated(t *testing.T) {
	cases := []struct {
		name, key, message string
		ctx                map[string]any
	}{
		{
			name: "Hailuo duration outside model tiers", key: "hailuo", message: "duration must be 6 or 10",
			ctx: map[string]any{"upstreamModel": "MiniMax-Hailuo-2.3", "apiKey": "key", "baseUrl": "https://example.test", "requestBody": map[string]any{"prompt": "p", "duration": 7}},
		},
		{
			name: "Jimeng frames", key: "jimeng", message: "frames must be 121 or 241",
			ctx: map[string]any{"upstreamModel": "jimeng_t2v_v30", "apiKey": "key", "baseUrl": "https://example.test", "requestBody": map[string]any{"prompt": "p", "metadata": map[string]any{"frames": 999}}},
		},
		{
			name: "Sora unsupported size", key: "sora", message: "size is not supported",
			ctx: map[string]any{"upstreamModel": "sora-2", "apiKey": "key", "baseUrl": "https://example.test", "requestBody": map[string]any{"prompt": "p", "size": "640x480"}},
		},
		{
			name: "Vidu fixed duration", key: "vidu", message: "viduq1 duration must be 5",
			ctx: map[string]any{"upstreamModel": "viduq1", "apiKey": "key", "baseUrl": "https://example.test", "requestBody": map[string]any{"model": "viduq1", "prompt": "p", "duration": 6}},
		},
		{
			name: "Doubao duration above model maximum", key: "doubao", message: "duration",
			ctx: map[string]any{"upstreamModel": "doubao-seedance-2-5-260628", "apiKey": "key", "baseUrl": "https://example.test", "requestBody": map[string]any{"model": "doubao-seedance-2-5-260628", "prompt": "p", "seconds": 31, "metadata": map[string]any{"resolution": "720p"}}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := callBuiltinTaskHook(t, tc.key, "buildSubmitRequest", tc.ctx)
			require.ErrorContains(t, err, tc.message)
		})
	}
}
