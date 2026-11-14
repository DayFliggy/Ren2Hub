package plugins_test

import (
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const seedream55Model = "doubao-seedream-5-5-261028"

func TestSeedream55LayerReservationAcrossImageProtocols(t *testing.T) {
	registry, plugin := newDoubaoPlugin(t)
	for _, tc := range []struct {
		name      string
		maxImages any
		want      float64
	}{{"default", nil, 30}, {"minimum", 2, 2}, {"maximum", 100, 100}} {
		for _, size := range []string{"1K", "1.5K", "2K", "auto"} {
			t.Run(tc.name+"/"+size, func(t *testing.T) {
				body := map[string]any{"model": seedream55Model, "image": "https://reference.example/image.png", "layer_decomposition": true, "size": size}
				fields := map[string][]string{"model": {seedream55Model}, "image": {"https://reference.example/image.png"}, "layer_decomposition": {"true"}, "size": {size}}
				if tc.maxImages != nil {
					body["layer_decomposition_options"] = map[string]any{"max_images": tc.maxImages}
					fields["layer_decomposition_options"] = []string{fmt.Sprintf(`{"max_images":%v}`, tc.maxImages)}
				}
				native, err := decodeDoubaoImage(t, registry, plugin, body)
				require.NoError(t, err)
				for name, wire := range map[string]map[string]any{
					"json":      {"kind": "json", "value": body},
					"multipart": {"kind": "multipart", "fields": fields},
				} {
					t.Run(name, func(t *testing.T) {
						decoded, err := decodeDoubaoOpenAIImage(t, plugin, "edit", seedream55Model, wire)
						require.NoError(t, err)
						assert.Equal(t, native["requestBody"], decoded["requestBody"])
						outbound, facts, _ := submitDoubaoImage(t, plugin, "image_to_image", decoded["requestBody"].(map[string]any))
						assert.Equal(t, alibabaObject(t, body), outbound)
						lower, higher := 0.0, tc.want
						if size == "1K" || size == "1.5K" {
							lower, higher = tc.want, 0
						}
						assert.Equal(t, map[string]any{"images_up_to_1_5k": lower, "images_above_1_5k": higher, "input_images": float64(1), "layer_decomposition": true}, facts)
					})
				}
			})
		}
	}
}

func TestSeedream55RejectsInvalidLayerOptionsBeforeReservation(t *testing.T) {
	registry, plugin := newDoubaoPlugin(t)
	for _, value := range []any{-1, 0, 1, 101, 2.5, "2", true, []any{2}, map[string]any{}, nil} {
		t.Run(fmt.Sprintf("%T_%v", value, value), func(t *testing.T) {
			body := map[string]any{"model": seedream55Model, "image": "https://reference.example/image.png", "layer_decomposition": true, "layer_decomposition_options": map[string]any{"max_images": value}}
			_, err := decodeDoubaoImage(t, registry, plugin, body)
			require.ErrorContains(t, err, "max_images must be an integer between 2 and 100")
			_, err = decodeDoubaoOpenAIImage(t, plugin, "edit", seedream55Model, map[string]any{"kind": "json", "value": body})
			require.ErrorContains(t, err, "max_images must be an integer between 2 and 100")
		})
	}
}

func TestSeedream55TransparencySizesAndReferenceBoundaries(t *testing.T) {
	registry, plugin := newDoubaoPlugin(t)
	for _, tc := range []struct {
		name      string
		fields    map[string]any
		wantLower float64
		wantError string
	}{
		{"transparent text", map[string]any{"background": "transparent", "output_format": "png", "size": "1K"}, 1, ""},
		{"automatic transparency", map[string]any{"background": "auto", "size": "1.5K"}, 1, ""},
		{"lower pixel boundary", map[string]any{"size": "1500x1740"}, 1, ""},
		{"above pixel boundary", map[string]any{"size": "1500x1741"}, 0, ""},
		{"smallest pixel area", map[string]any{"size": "960x960"}, 1, ""},
		{"below smallest area", map[string]any{"size": "959x960"}, 0, "pixel and aspect-ratio limits"},
		{"oversized preset", map[string]any{"size": "4K"}, 0, "size must be one of"},
		{"transparent jpeg", map[string]any{"background": "transparent", "output_format": "jpeg"}, 0, "cannot be combined"},
		{"no reference for layers", map[string]any{"layer_decomposition": true}, 0, "exactly one input image"},
		{"two references for layers", map[string]any{"layer_decomposition": true, "image": []any{"https://reference.example/a", "https://reference.example/b"}}, 0, "exactly one input image"},
		{"fast prompt unsupported", map[string]any{"optimize_prompt_options": map[string]any{"mode": "fast"}}, 0, "fast is not supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]any{"model": seedream55Model, "prompt": "a glass cat"}
			maps.Copy(body, tc.fields)
			decoded, err := decodeDoubaoImage(t, registry, plugin, body)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			_, facts, _ := submitDoubaoImage(t, plugin, decoded["action"].(string), decoded["requestBody"].(map[string]any))
			assert.Equal(t, tc.wantLower, facts["images_up_to_1_5k"])
			assert.Equal(t, 1-tc.wantLower, facts["images_above_1_5k"])
		})
	}
	for _, count := range []int{10, 11} {
		images := make([]any, count)
		for i := range images {
			images[i] = fmt.Sprintf("https://reference.example/%d.png", i)
		}
		body := map[string]any{"model": seedream55Model, "prompt": "merge", "image": images, "background": "transparent", "size": "1K"}
		decoded, err := decodeDoubaoImage(t, registry, plugin, body)
		if count == 11 {
			require.ErrorContains(t, err, "at most 10 reference images")
			continue
		}
		require.NoError(t, err)
		_, facts, _ := submitDoubaoImage(t, plugin, "image_to_image", decoded["requestBody"].(map[string]any))
		assert.Equal(t, float64(10), facts["input_images"])
		assert.Equal(t, float64(1), facts["images_up_to_1_5k"], "references must not multiply the output count")
	}
}

func TestSeedream55KeepsOlderImageAndVideoBoundaries(t *testing.T) {
	registry, plugin := newDoubaoPlugin(t)
	for _, model := range []string{"doubao-seedream-5-0-pro-260628", "doubao-seedream-5-0-flash-260915"} {
		decoded, err := decodeDoubaoImage(t, registry, plugin, map[string]any{"model": model, "image": "https://reference.example/image.png", "layer_decomposition": true})
		require.NoError(t, err)
		_, facts, _ := submitDoubaoImage(t, plugin, "image_to_image", decoded["requestBody"].(map[string]any))
		assert.Equal(t, float64(17), facts["images_above_1_5k"])
		_, err = decodeDoubaoImage(t, registry, plugin, map[string]any{"model": model, "prompt": "cat", "background": "transparent"})
		require.ErrorContains(t, err, "exactly one input image")
		_, err = decodeDoubaoImage(t, registry, plugin, map[string]any{"model": model, "prompt": "cat", "background": "auto"})
		require.ErrorContains(t, err, "background must be opaque or transparent")
	}
	for _, model := range []string{"doubao-seedream-4-0-250828", "doubao-seedream-4-5-251128", "doubao-seedream-5-0-lite-260128"} {
		_, err := decodeDoubaoImage(t, registry, plugin, map[string]any{"model": model, "image": "https://reference.example/image.png", "layer_decomposition": true})
		require.ErrorContains(t, err, "layer_decomposition is not supported")
	}
	for _, hook := range []string{"buildSubmitRequest", "extractUsage"} {
		_, err := plugin.Engine.Call(t.Context(), hook, map[string]any{"model": "doubao-seedance-2-5-260628", "upstreamModel": "doubao-seedance-2-5-260628", "requestBody": map[string]any{"prompt": "cat", "seconds": 31}})
		require.ErrorContains(t, err, "duration must be a positive integer no greater than 30 or -1")
	}
}
