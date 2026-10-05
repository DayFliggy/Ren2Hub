package plugins_test

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKlingResponsesProtocol(t *testing.T) {
	testVideoResponsesProtocol(t, videoResponsesTestCase{
		pluginKey: "kling",
		model:     "kling-v2-master",
		requestBody: map[string]any{
			"model": "kling-v2-master",
			"input": []any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "camera orbit"},
				map[string]any{"type": "input_image", "image_url": "https://cdn.example/frame.png"},
			}}},
			"seconds": 10,
			"metadata": map[string]any{
				"mode": "pro",
			},
		},
		wantAction: "image_to_video",
		wantRequest: map[string]any{
			"model":    "kling-v2-master",
			"prompt":   "camera orbit",
			"image":    "https://cdn.example/frame.png",
			"duration": float64(10),
			"metadata": map[string]any{"mode": "pro"},
		},
		wantUsageKeys:  []string{"units"},
		wantVendorName: "kling",
	})
}

func TestKlingSubmitEstimatePreservesDecimalForSupportedDurations(t *testing.T) {
	source, err := builtinplugins.Source("kling")
	require.NoError(t, err)
	plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	for _, test := range []struct {
		duration int
		want     string
	}{
		{duration: 3, want: "0.6"},
		{duration: 6, want: "1.2"},
		{duration: 7, want: "1.4"},
		{duration: 12, want: "2.4"},
	} {
		t.Run(strconv.Itoa(test.duration)+"s", func(t *testing.T) {
			value, callErr := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{
				"model":         "kling-v1",
				"upstreamModel": "kling-v1",
				"requestBody":   map[string]any{"model": "kling-v1", "duration": test.duration},
			})
			require.NoError(t, callErr)
			encoded, marshalErr := common.Marshal(value)
			require.NoError(t, marshalErr)
			var facts map[string]any
			require.NoError(t, common.Unmarshal(encoded, &facts))
			assert.Equal(t, test.want, facts["units"])
		})
	}
}
