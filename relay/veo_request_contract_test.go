package relay

import (
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVeoPluginRequestAndFrozenUsageContract(t *testing.T) {
	for _, key := range []string{"google", "vertex-ai"} {
		t.Run(key, func(t *testing.T) {
			source, err := plugins.Source(key)
			require.NoError(t, err)
			plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
			require.NoError(t, err)
			context := map[string]any{"baseUrl": "https://provider.example", "upstreamModel": "veo-3.1-generate-preview", "apiKey": "fixture", "authHeader": "Bearer fixture", "auth": map[string]any{"projectId": "project"}, "requestBody": map[string]any{"prompt": "fixture", "duration": 6, "metadata": map[string]any{"resolution": "4K", "seed": 0, "generateAudio": false}}}
			request, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", context)
			require.NoError(t, err)
			wire := request.(map[string]any)["body"].(map[string]any)["parameters"].(map[string]any)
			assert.EqualValues(t, 6, wire["durationSeconds"])
			assert.Equal(t, "4k", wire["resolution"])
			assert.EqualValues(t, 0, wire["seed"])
			assert.Equal(t, false, wire["generateAudio"])
			facts, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete", nil, nil, map[string]any{})
			require.NoError(t, err)
			assert.Nil(t, facts, "missing actual duration must not be invented")
			_, err = plugins.FrozenTaskPriceVersion(plugin.Meta, plugin.Meta.Models[0], `u("seconds") * 0.1`)
			require.Error(t, err, "estimated duration cannot become a final charge")
		})
	}
}
