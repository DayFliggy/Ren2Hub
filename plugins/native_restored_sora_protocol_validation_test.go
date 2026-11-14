//go:build known_upstream_restored

package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestoredProbeSoraVideoProtocolPreservesDurationConflictForValidation(t *testing.T) {
	source, err := builtinplugins.Source("sora")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "sora"})
	require.NoError(t, err)
	for _, kind := range []string{"json", "multipart"} {
		for _, duration := range []string{"4", "8"} {
			t.Run(kind+"/duration="+duration, func(t *testing.T) {
				body := map[string]any{"kind": kind}
				if kind == "json" {
					body["value"] = map[string]any{"prompt": "a cat", "seconds": 4, "duration": duration}
				} else {
					body["fields"] = map[string]any{"prompt": []string{"a cat"}, "seconds": []string{"4"}, "duration": []string{duration}}
					body["files"] = []any{map[string]any{"field": "input_reference", "ref": "file-0", "filename": "frame.png"}}
				}
				decoded, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{
					"model": "sora-2", "body": body,
				})
				require.NoError(t, err)
				resolved := decodePluginValue(t, decoded)
				request := resolved["requestBody"].(map[string]any)
				assert.Equal(t, duration, request["duration"])
				ctx := map[string]any{"upstreamModel": "sora-2", "action": resolved["action"], "requestBody": request}
				if kind == "multipart" {
					ctx["files"] = body["files"]
				}
				for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
					value, err := plugin.Engine.Call(t.Context(), hook, ctx)
					if duration == "8" {
						require.ErrorContains(t, err, "seconds and duration must match", hook)
						continue
					}
					require.NoError(t, err, hook)
					if hook == "extractUsage" {
						assert.Equal(t, float64(4), decodePluginValue(t, value)["seconds"])
					}
				}
			})
		}
	}
}
