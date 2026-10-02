package plugins_test

import (
	"bytes"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const seedreamFlash = "doubao-seedream-5-0-flash-260915"

func decodeDoubaoImagesProtocol(t *testing.T, plugin *jsplugin.LoadedPlugin, operation string, body map[string]any) (map[string]any, error) {
	t.Helper()
	value, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_image", "decodeRequest"}, map[string]any{
		"protocol": "openai_image", "operation": operation, "model": body["model"], "body": map[string]any{"kind": "json", "value": body},
	})
	if err != nil {
		return nil, err
	}
	return alibabaObject(t, value), nil
}

func TestDoubaoSeedreamProfilesAdvertiseOnlyModelCapabilities(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	for _, test := range []struct {
		model  string
		fields []string
	}{
		{seedreamFlash, []string{"images_up_to_1_5k", "images_above_1_5k", "input_images", "layer_decomposition"}},
		{"doubao-seedream-5-0-pro-260628", []string{"images_up_to_1_5k", "images_above_1_5k", "input_images", "layer_decomposition"}},
		{"doubao-seedream-5-0-lite-260128", []string{"images_above_1_5k", "input_images"}},
		{"doubao-seedream-5-0-260128", []string{"images_above_1_5k", "input_images"}},
		{"doubao-seedream-4-5-251128", []string{"images_above_1_5k", "input_images"}},
		{"doubao-seedream-4-0-250828", []string{"images_up_to_1_5k", "images_above_1_5k", "input_images"}},
	} {
		t.Run(test.model, func(t *testing.T) {
			display, examples := plugin.Meta.UsageForModel(test.model)
			assert.ElementsMatch(t, test.fields, keysOf(display))
			for _, example := range examples {
				assert.ElementsMatch(t, test.fields, keysOfFacts(example.Facts))
			}
			assert.ElementsMatch(t, []string{"images_up_to_1_5k", "images_above_1_5k", "input_images", "layer_decomposition"}, keysOf(plugin.Meta.UsageFactsForModels("ep-mapped", test.model)))
		})
	}
}

func keysOfFacts(facts map[string]any) []string {
	keys := make([]string, 0, len(facts))
	for key := range facts {
		keys = append(keys, key)
	}
	return keys
}

func TestDoubaoOpenAIImagesJSONUsesSharedNativeValidation(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	for _, test := range []struct {
		name, operation, model string
		request                map[string]any
		wantError              string
	}{
		{"flash generation", "generate", seedreamFlash, map[string]any{"prompt": "cat", "size": "1.5K", "n": 1, "response_format": "b64_json"}, ""},
		{"URL edit", "edit", seedreamFlash, map[string]any{"prompt": "watercolor", "image": "https://cdn.example/source.png"}, ""},
		{"data URI edit", "edit", seedreamFlash, map[string]any{"prompt": "watercolor", "image": "data:image/png;base64,QUFB"}, ""},
		{"flash layer decomposition", "edit", seedreamFlash, map[string]any{"image": "https://cdn.example/source.png", "layer_decomposition": true, "size": "auto"}, ""},
		{"edit needs reference", "edit", seedreamFlash, map[string]any{"prompt": "cat"}, "image is required"},
		{"stream unsupported", "generate", seedreamFlash, map[string]any{"prompt": "cat", "stream": true}, "stream is not supported"},
		{"mask unsupported", "edit", seedreamFlash, map[string]any{"prompt": "cat", "image": "https://cdn.example/a.png", "mask": "https://cdn.example/m.png"}, "mask is not supported"},
		{"batch count cannot bypass group API", "generate", seedreamFlash, map[string]any{"prompt": "cat", "n": 2}, "n must be 1"},
		{"fractional count", "generate", seedreamFlash, map[string]any{"prompt": "cat", "n": 1.5}, "n must be 1"},
		{"flash rejects fast prompt", "generate", seedreamFlash, map[string]any{"prompt": "cat", "optimize_prompt_options": map[string]any{"mode": "fast"}}, "not supported by this model"},
		{"flash rejects 4K", "generate", seedreamFlash, map[string]any{"prompt": "cat", "size": "4K"}, "size must be one of"},
		{"flash rejects groups", "generate", seedreamFlash, map[string]any{"prompt": "cat", "sequential_image_generation": "auto"}, "not supported by this model"},
		{"lite rejects low tier", "generate", "doubao-seedream-5-0-lite-260128", map[string]any{"prompt": "cat", "size": "1K"}, "size must be one of"},
		{"group count is bounded", "generate", "doubao-seedream-4-5-251128", map[string]any{"prompt": "cat", "sequential_image_generation": "auto", "sequential_image_generation_options": map[string]any{"max_images": 16}}, "outside declared bounds"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := map[string]any{"model": test.model}
			maps.Copy(body, test.request)
			intent, err := decodeDoubaoImagesProtocol(t, plugin, test.operation, body)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			request := intent["requestBody"].(map[string]any)
			assert.NotContains(t, request, "response_format", "the host owns Base64 output")
			assert.NotContains(t, request, "n", "one Ark request must not become multiple submissions")
			upstream, facts, url := submitDoubaoImage(t, plugin, intent["action"].(string), request)
			assert.Equal(t, doubaoBaseURL+"/api/v3/images/generations", url)
			assert.Equal(t, test.model, upstream["model"])
			assert.NotEmpty(t, facts)
		})
	}
}

func TestDoubaoOpenAIImagesMultipartResolvesBoundedUploads(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", seedreamFlash))
	require.NoError(t, writer.WriteField("prompt", "watercolor"))
	require.NoError(t, writer.WriteField("watermark", "false"))
	file, err := writer.CreatePart(textproto.MIMEHeader{"Content-Disposition": {`form-data; name="image[]"; filename="input.png"`}, "Content-Type": {"image/png"}})
	require.NoError(t, err)
	_, err = file.Write([]byte("AAA"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	decodedBody := map[string]any{"kind": "multipart", "fields": map[string][]string{"model": {seedreamFlash}, "prompt": {"watercolor"}, "watermark": {"false"}, "n": {"1"}},
		"files": []any{map[string]any{"ref": "request_file:image[]", "field": "image[]", "mimeType": "image/png", "size": 3}}}
	ctx := map[string]any{"model": seedreamFlash, "protocol": "openai_image", "operation": "edit", "body": decodedBody}
	value, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_image", "decodeRequest"}, ctx)
	require.NoError(t, err)
	intent := alibabaObject(t, value)
	assert.Equal(t, "image_to_image", intent["action"])
	info := &relaycommon.RelayInfo{OriginModelName: seedreamFlash, ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: doubaoBaseURL, UpstreamModelName: seedreamFlash}, TaskRelayInfo: &relaycommon.TaskRelayInfo{Action: "image_to_image"}}
	adaptor := taskplugin.New(plugin)
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Set("task_request", exactUsageRequest(t, intent["requestBody"].(map[string]any)))
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	var upstream map[string]any
	require.NoError(t, common.Unmarshal(encoded, &upstream))
	assert.Equal(t, []any{"data:image/png;base64,QUFB"}, upstream["image"])
	assert.Equal(t, false, upstream["watermark"])
	assert.NotContains(t, string(encoded), "__fileRef")
	facts, err := adaptor.ExtractUsageFactsValidated(c, info)
	require.NoError(t, err)
	assert.Equal(t, "1", facts["input_images"])

	decodedBody["fields"] = map[string][]string{"prompt": {"one", "two"}}
	_, err = plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_image", "decodeRequest"}, ctx)
	require.ErrorContains(t, err, "prompt must be provided once")
	decodedBody["fields"] = map[string][]string{"prompt": {"cat"}}
	decodedBody["files"] = []any{map[string]any{"ref": "request_file:mask", "field": "mask", "mimeType": "image/png"}}
	_, err = plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_image", "decodeRequest"}, ctx)
	require.ErrorContains(t, err, "mask is not supported")
}

func TestDoubaoSingleTierCompletionCountsOnlyDeliveredImagesWithoutSizes(t *testing.T) {
	_, plugin := newDoubaoPlugin(t)
	for _, modelName := range []string{"doubao-seedream-5-0-lite-260128", "doubao-seedream-5-0-260128", "doubao-seedream-4-5-251128"} {
		value, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete", map[string]any{"model": modelName, "upstreamModel": "ep-mapped"}, map[string]any{"status": "SUCCESS"}, map[string]any{
			"data":  []any{map[string]any{"url": "https://cdn.example/1.png"}, map[string]any{"error": map[string]any{"code": "blocked"}}, map[string]any{"url": "https://cdn.example/2.png"}},
			"usage": map[string]any{"generated_images": 2},
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"images_up_to_1_5k": float64(0), "images_above_1_5k": float64(2)}, alibabaObject(t, value), modelName)
	}
}
