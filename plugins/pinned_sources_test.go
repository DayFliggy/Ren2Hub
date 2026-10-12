package plugins_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tasks submitted under the 10ccd9a2f release replay their exact stored plugin
// source, so those bytes must keep compiling and settling on the current
// runtime. The fixtures are byte copies of plugins/tasks at 10ccd9a2f.
const pinnedSourceDir = "testdata/pinned-10ccd9a2f"

func pinnedPlugin(t *testing.T, key string) (*jsplugin.LoadedPlugin, string) {
	t.Helper()
	source, err := os.ReadFile(filepath.Join(pinnedSourceDir, key+".js"))
	require.NoError(t, err)
	plugin, err := jsplugin.CompilePlugin(string(source), jsplugin.Options{})
	require.NoError(t, err)
	return plugin, string(source)
}

// Every deployed release stores its factory rows permanently, and startup
// refuses a key and version whose stored source differs. Each pinned-<sha>
// directory holds the byte copies shipped by one deployed release.
func TestReleasedFactorySourcesCompileAndKeepTheirIdentity(t *testing.T) {
	// Built-in plugins each pinned release shipped.
	releasedPlugins := map[string]int{"pinned-10ccd9a2f": 10, "pinned-1bb6e849e": 10, "pinned-12435f4d5": 11, "pinned-ce505c9b5": 11}
	releases, err := filepath.Glob("testdata/pinned-*")
	require.NoError(t, err)
	require.Len(t, releases, len(releasedPlugins))
	for _, release := range releases {
		entries, err := os.ReadDir(release)
		require.NoError(t, err)
		require.Len(t, entries, releasedPlugins[filepath.Base(release)], release)
		for _, entry := range entries {
			key := strings.TrimSuffix(entry.Name(), ".js")
			t.Run(filepath.Base(release)+"/"+key, func(t *testing.T) {
				released, err := os.ReadFile(filepath.Join(release, entry.Name()))
				require.NoError(t, err)
				plugin, err := jsplugin.CompilePlugin(string(released), jsplugin.Options{})
				require.NoError(t, err)
				assert.Equal(t, key, plugin.Meta.Key)

				current, err := builtinplugins.Source(key)
				require.NoError(t, err)
				currentPlugin, err := jsplugin.CompilePlugin(current, jsplugin.Options{})
				require.NoError(t, err)
				if currentPlugin.Meta.Version == plugin.Meta.Version {
					assert.Equal(t, string(released), current, "released version %s was republished with different source", plugin.Meta.Version)
				}
			})
		}
	}
}

func TestReleasedFactorySourcesSettleExactProviderUnits(t *testing.T) {
	for _, tc := range []struct {
		key        string
		task, body map[string]any
		field      string
		want       any
	}{
		{"vidu", nil, map[string]any{"credits": json.Number("3.500000000000000001")}, "credits", "3.500000000000000001"},
		{"kling", nil, map[string]any{"data": map[string]any{"final_unit_deduction": json.Number("3.5")}}, "units", "3.5"},
		{"sora", nil, map[string]any{"seconds": json.Number("4")}, "seconds", "4"},
		{"alibaba", map[string]any{"upstreamModel": "wan3.0-video"}, map[string]any{"usage": map[string]any{"input_video_duration": json.Number("0.1"), "output_video_duration": json.Number("0.2")}}, "seconds", "0.3"},
		{"doubao", map[string]any{"upstreamModel": "doubao-seedance-2-0-260128"}, map[string]any{"status": "succeeded", "usage": map[string]any{"completion_tokens": json.Number("1001")}}, "tokens", "1001"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			plugin, _ := pinnedPlugin(t, tc.key)
			result, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete", tc.task, nil, tc.body)
			require.NoError(t, err)
			facts, ok := result.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tc.want, facts[tc.field])
		})
	}
	for _, key := range []string{"vidu", "kling", "google", "vertex-ai", "jimeng"} {
		t.Run(key+"/missing", func(t *testing.T) {
			plugin, _ := pinnedPlugin(t, key)
			result, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete", nil, nil, map[string]any{})
			require.NoError(t, err)
			assert.Nil(t, result)
		})
	}
}
