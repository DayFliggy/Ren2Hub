package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A vendor video request may omit the duration and let the provider apply its
// own default. The submit builder must not run the decimal boundary on an
// absent value, which rejects anything that is not decimal text or an exact
// number and would turn an omitted field into a 400.
func TestBuiltInVideoSubmitWithoutDurationUsesProviderDefault(t *testing.T) {
	for _, tc := range []struct{ key, upstreamModel string }{
		{"vertex-ai", "veo-3.0-generate-001"},
		{"google", "veo-3.0-generate-001"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			source, err := builtinplugins.Source(tc.key)
			require.NoError(t, err)
			plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
			require.NoError(t, err)

			context := map[string]any{
				"requestBody":   map[string]any{"prompt": "a cat"},
				"model":         "veo-3.0",
				"upstreamModel": tc.upstreamModel,
				"action":        "text_to_video",
				"baseUrl":       "https://example.invalid",
				"userSetting":   map[string]any{"vertexProjectId": "p", "geminiVersion": "v1beta"},
				"auth":          map[string]any{"projectId": "p"},
				"authHeader":    "Bearer test",
			}
			result, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", context)
			require.NoError(t, err)
			descriptor, ok := result.(map[string]any)
			require.True(t, ok)

			body, ok := descriptor["body"].(map[string]any)
			require.True(t, ok)
			parameters, ok := body["parameters"].(map[string]any)
			require.True(t, ok)
			assert.NotContains(t, parameters, "durationSeconds", "an omitted duration must stay omitted")
		})
	}
}

func TestBuiltInVideoSubmitUsesExplicitDuration(t *testing.T) {
	plugin := compileBuiltInPlugin(t, "vertex-ai")
	result, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
		"requestBody":   map[string]any{"prompt": "a cat", "duration": 8},
		"model":         "veo-3.0",
		"upstreamModel": "veo-3.0-generate-001",
		"action":        "text_to_video",
		"baseUrl":       "https://example.invalid",
		"userSetting":   map[string]any{"vertexProjectId": "p"},
		"auth":          map[string]any{"projectId": "p"},
		"authHeader":    "Bearer test",
	})
	require.NoError(t, err)
	body := result.(map[string]any)["body"].(map[string]any)
	assert.Equal(t, int64(8), parameters(body)["durationSeconds"])
}

func compileBuiltInPlugin(t *testing.T, key string) *jsplugin.LoadedPlugin {
	t.Helper()
	source, err := builtinplugins.Source(key)
	require.NoError(t, err)
	plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	return plugin
}

func parameters(body map[string]any) map[string]any {
	value, _ := body["parameters"].(map[string]any)
	return value
}
