package controller

import (
	"bytes"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Frontend contract fixtures are generated from real handlers so Vue specs and
// Playwright mocks consume the backend's actual shapes instead of hand-written
// copies. Regenerate with:
//
//	go test ./controller -run TestFrontendContract -update
var updateFrontendContract = flag.Bool("update", false, "rewrite frontend/tests/fixtures/native")

const frontendContractDir = "../frontend/tests/fixtures/native"

// frontendFixture writes (with -update) or compares the canonical JSON form of
// value and returns the committed bytes, so request tests send exactly the file
// that frontend tests import.
func frontendFixture(t *testing.T, name string, value any) []byte {
	t.Helper()
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	// Decoding into any and re-encoding sorts object keys deterministically.
	var decoded any
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	encoded, err = common.Marshal(decoded)
	require.NoError(t, err)
	canonical, err := common.IndentJson(encoded)
	require.NoError(t, err)
	canonical = append(canonical, '\n')
	path := filepath.Join(frontendContractDir, name)
	if *updateFrontendContract {
		require.NoError(t, os.MkdirAll(frontendContractDir, 0o755))
		require.NoError(t, os.WriteFile(path, canonical, 0o644))
		return canonical
	}
	committed, err := os.ReadFile(path)
	require.NoError(t, err, "missing fixture; run go test ./controller -run TestFrontendContract -update")
	require.Equal(t, string(canonical), string(committed), "stale fixture %s; run go test ./controller -run TestFrontendContract -update", name)
	return committed
}

// contractEngine mounts real handlers on their real method and path behind a
// stand-in for the auth middleware that sets the operator identity.
func contractEngine(userID, role int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", userID)
		c.Set("role", role)
		c.Next()
	})
	return engine
}

func contractRequest(t *testing.T, engine *gin.Engine, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, request)
	return recorder
}

func decodeContractBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body), recorder.Body.String())
	return body
}

var pricingContractModels = []string{"contract-chat", "contract-image"}

// pricingContractEnvelope keeps only the fixture models. Static default ratios
// and built-in tiered expressions list every known model and change with each
// default refresh, so they are not part of this contract. Versions are left
// untouched because they are the optimistic lock the PATCH must echo.
func pricingContractEnvelope(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	data := body["data"].(map[string]any)
	entries := []any{}
	for _, raw := range data["entries"].([]any) {
		if slices.Contains(pricingContractModels, raw.(map[string]any)["model_name"].(string)) {
			entries = append(entries, raw)
		}
	}
	options := map[string]any{}
	for key, raw := range data["options"].(map[string]any) {
		var values map[string]any
		require.NoError(t, common.UnmarshalJsonStr(raw.(string), &values))
		kept := map[string]any{}
		for name, value := range values {
			modelName := name
			if key == billing_setting.PluginBillingExprOption {
				_, modelName, _ = billing_setting.SplitPluginBillingExprKey(name)
			}
			if slices.Contains(pricingContractModels, modelName) {
				kept[name] = value
			}
		}
		encoded, err := common.Marshal(kept)
		require.NoError(t, err)
		options[key] = string(encoded)
	}
	data["entries"], data["options"] = entries, options
	return body
}

func pricingContractVersion(t *testing.T, body map[string]any, name string) string {
	t.Helper()
	for _, raw := range body["data"].(map[string]any)["entries"].([]any) {
		entry := raw.(map[string]any)
		if entry["model_name"] == name {
			return entry["version"].(string)
		}
	}
	require.Failf(t, "missing pricing entry", "%s", name)
	return ""
}

func TestFrontendContractModelPricing(t *testing.T) {
	modelManagementDB(t, "sqlite", "")
	empty := model.ModelPricingVersion(model.PricingValues{})
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{
		{ModelName: "contract-chat", ExpectedVersion: empty, Pricing: model.PricingValues{"ModelRatio": 2.5, "CompletionRatio": 4.0}},
		{ModelName: "contract-image", ExpectedVersion: empty, Pricing: model.PricingValues{"ModelPrice": 0.04}},
	}))
	engine := contractEngine(1, common.RoleRootUser)
	engine.GET("/api/option/model_pricing", GetModelPricingConfig)
	engine.PATCH("/api/option/model_pricing", UpdateModelPricingConfig)

	loaded := contractRequest(t, engine, http.MethodGet, "/api/option/model_pricing", nil)
	require.Equal(t, http.StatusOK, loaded.Code, loaded.Body.String())
	snapshot := pricingContractEnvelope(t, decodeContractBody(t, loaded))
	frontendFixture(t, "model-pricing.get.json", snapshot)
	chatVersion := pricingContractVersion(t, snapshot, "contract-chat")

	// The pricing editor raises the input price from $5/M to $6/M and keeps the
	// 4x output ratio; the UI must send both as JSON numbers.
	patch := frontendFixture(t, "model-pricing.patch.request.json", map[string]any{
		"changes": []any{map[string]any{
			"model_name":       "contract-chat",
			"expected_version": chatVersion,
			"pricing":          map[string]any{"ModelRatio": 3, "CompletionRatio": 4},
		}},
	})

	var stringPrice map[string]any
	require.NoError(t, common.Unmarshal(patch, &stringPrice))
	stringPrice["changes"].([]any)[0].(map[string]any)["pricing"].(map[string]any)["ModelRatio"] = "3"
	encoded, err := common.Marshal(stringPrice)
	require.NoError(t, err)
	rejected := contractRequest(t, engine, http.MethodPatch, "/api/option/model_pricing", encoded)
	require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
	assert.Contains(t, rejected.Body.String(), "must be a finite, non-negative number")
	unchanged := contractRequest(t, engine, http.MethodGet, "/api/option/model_pricing", nil)
	assert.Equal(t, chatVersion, pricingContractVersion(t, decodeContractBody(t, unchanged), "contract-chat"))

	saved := contractRequest(t, engine, http.MethodPatch, "/api/option/model_pricing", patch)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	assert.Equal(t, map[string]any{"updated_models": []any{"contract-chat"}}, decodeContractBody(t, saved)["data"])
	reloaded := decodeContractBody(t, contractRequest(t, engine, http.MethodGet, "/api/option/model_pricing", nil))
	assert.NotEqual(t, chatVersion, pricingContractVersion(t, reloaded, "contract-chat"))
	for _, raw := range reloaded["data"].(map[string]any)["entries"].([]any) {
		if entry := raw.(map[string]any); entry["model_name"] == "contract-chat" {
			assert.Equal(t, map[string]any{"ModelRatio": 3.0, "CompletionRatio": 4.0}, entry["configured"])
		}
	}
}

func TestFrontendContractVertexChannelUpdate(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	baseURL, priority, weight := "", int64(5), uint(2)
	region := `{"default":"us-central1"}`
	channel := model.Channel{
		Type: constant.ChannelTypeVertexAi, Name: "vertex-contract", Key: `{"type":"service_account"}`,
		Models: "gemini-2.5-pro", Other: region, OtherSettings: `{"vertex_key_type":"json"}`,
		BaseURL: &baseURL, Priority: &priority, Weight: &weight, ChannelRatio: "1", Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())
	saved := func() model.Channel {
		var current model.Channel
		require.NoError(t, model.DB.First(&current, channel.Id).Error)
		return current
	}

	// A rename from the edit form with ChannelSensitiveWrite resends the whole
	// sensitive group, including the Vertex region that validateChannel needs.
	rename := frontendFixture(t, "channel-vertex-rename.put.request.json", map[string]any{
		"id": channel.Id, "name": "vertex-contract-renamed", "type": constant.ChannelTypeVertexAi,
		"base_url": "", "models": "gemini-2.5-pro", "model_mapping": "", "priority": 5, "weight": 2,
		"channel_ratio": "1", "max_concurrency": 0, "other": region,
	})
	root := contractEngine(1, common.RoleRootUser)
	root.PUT("/api/channel/", UpdateChannel)

	var withoutRegion map[string]any
	require.NoError(t, common.Unmarshal(rename, &withoutRegion))
	delete(withoutRegion, "other")
	encoded, err := common.Marshal(withoutRegion)
	require.NoError(t, err)
	rejected := contractRequest(t, root, http.MethodPut, "/api/channel/", encoded)
	assert.Contains(t, rejected.Body.String(), `"success":false`)
	assert.Contains(t, rejected.Body.String(), "Deployment region cannot be empty")
	assert.Equal(t, "vertex-contract", saved().Name)

	updated := contractRequest(t, root, http.MethodPut, "/api/channel/", rename)
	require.Contains(t, updated.Body.String(), `"success":true`, updated.Body.String())
	current := saved()
	assert.Equal(t, "vertex-contract-renamed", current.Name)
	assert.Equal(t, region, current.Other)
	assert.Equal(t, constant.ChannelTypeVertexAi, current.Type)
	assert.Equal(t, "1", current.ChannelRatio)

	// Without ChannelSensitiveWrite the form omits the sensitive group entirely,
	// so UpdateChannel skips the Vertex check and keeps the stored values.
	writeOnly := frontendFixture(t, "channel-vertex-rename-write-only.put.request.json", map[string]any{
		"id": channel.Id, "name": "vertex-contract-admin", "models": "gemini-2.5-pro",
		"model_mapping": "", "priority": 5, "weight": 2, "max_concurrency": 0,
	})
	admin := contractEngine(2, common.RoleAdminUser)
	admin.PUT("/api/channel/", UpdateChannel)
	allowed := contractRequest(t, admin, http.MethodPut, "/api/channel/", writeOnly)
	require.Contains(t, allowed.Body.String(), `"success":true`, allowed.Body.String())
	current = saved()
	assert.Equal(t, "vertex-contract-admin", current.Name)
	assert.Equal(t, region, current.Other)
	assert.Equal(t, constant.ChannelTypeVertexAi, current.Type)
	assert.Equal(t, "1", current.ChannelRatio)
	assert.Equal(t, `{"vertex_key_type":"json"}`, current.OtherSettings)
}

func TestFrontendContractTaskPluginChannelUpdate(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	const pluginKey = "contract-plugin"
	_, err := jsplugin.DefaultRegistry.Register(`
export const meta = {apiVersion: 1, key: "contract-plugin", name: "Contract", version: "1.0.0", author: {name: "Test"}, models: ["doc"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { jsplugin.DefaultRegistry.Unregister(pluginKey) })
	baseURL, priority, weight := "https://plugin.example.com", int64(0), uint(1)
	setting := `{"task_plugin_key":"contract-plugin"}`
	channel := model.Channel{
		Type: constant.ChannelTypeTaskPlugin, Name: "plugin-contract", Key: "sk", Models: "doc",
		BaseURL: &baseURL, Setting: &setting, Priority: &priority, Weight: &weight,
		ChannelRatio: "1", Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())
	saved := func() model.Channel {
		var current model.Channel
		require.NoError(t, model.DB.First(&current, channel.Id).Error)
		return current
	}

	// validateChannel reads task_plugin_key from the request, so a sensitive
	// writer's rename must resend the unchanged setting with type 61.
	rename := frontendFixture(t, "channel-task-plugin-rename.put.request.json", map[string]any{
		"id": channel.Id, "name": "plugin-contract-renamed", "type": constant.ChannelTypeTaskPlugin,
		"base_url": baseURL, "models": "doc", "model_mapping": "", "priority": 0, "weight": 1,
		"channel_ratio": "1", "max_concurrency": 0, "setting": setting,
	})
	root := contractEngine(1, common.RoleRootUser)
	root.PUT("/api/channel/", UpdateChannel)
	var withoutSetting map[string]any
	require.NoError(t, common.Unmarshal(rename, &withoutSetting))
	delete(withoutSetting, "setting")
	encoded, err := common.Marshal(withoutSetting)
	require.NoError(t, err)
	rejected := contractRequest(t, root, http.MethodPut, "/api/channel/", encoded)
	assert.Contains(t, rejected.Body.String(), "task plugin key is required")
	assert.Equal(t, "plugin-contract", saved().Name)

	updated := contractRequest(t, root, http.MethodPut, "/api/channel/", rename)
	require.Contains(t, updated.Body.String(), `"success":true`, updated.Body.String())
	assert.Equal(t, "plugin-contract-renamed", saved().Name)

	// An admin without task_plugin.bind is refused whenever type 61 is sent, so
	// the form omits the sensitive group and the stored binding stays.
	admin := contractEngine(2, common.RoleAdminUser)
	admin.PUT("/api/channel/", UpdateChannel)
	refused := contractRequest(t, admin, http.MethodPut, "/api/channel/", rename)
	assert.Contains(t, refused.Body.String(), "task_plugin.bind")
	writeOnly := frontendFixture(t, "channel-task-plugin-rename-write-only.put.request.json", map[string]any{
		"id": channel.Id, "name": "plugin-contract-admin", "models": "doc",
		"model_mapping": "", "priority": 0, "weight": 1, "max_concurrency": 0,
	})
	allowed := contractRequest(t, admin, http.MethodPut, "/api/channel/", writeOnly)
	require.Contains(t, allowed.Body.String(), `"success":true`, allowed.Body.String())
	current := saved()
	assert.Equal(t, "plugin-contract-admin", current.Name)
	assert.Equal(t, constant.ChannelTypeTaskPlugin, current.Type)
	require.NotNil(t, current.Setting)
	assert.Equal(t, setting, *current.Setting)
	assert.Equal(t, baseURL, *current.BaseURL)
}

// The edit form keeps types outside its create list (here AI Proxy Library,
// which upstream web also leaves out) and edits only their common fields.
func TestFrontendContractNativeTypeChannelUpdate(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	baseURL, priority, weight := "https://library.example.com", int64(0), uint(1)
	const other, otherSettings = "contract-library", `{"allow_service_tier":true}`
	channel := model.Channel{
		Type: constant.ChannelTypeAIProxyLibrary, Name: "native-contract", Key: "sk", Models: "gpt-4o",
		Other: other, OtherSettings: otherSettings, BaseURL: &baseURL, Priority: &priority,
		Weight: &weight, ChannelRatio: "1", Status: common.ChannelStatusEnabled,
	}
	require.NoError(t, channel.Insert())

	rename := frontendFixture(t, "channel-native-type-rename.put.request.json", map[string]any{
		"id": channel.Id, "name": "native-contract-renamed", "type": constant.ChannelTypeAIProxyLibrary,
		"base_url": baseURL, "models": "gpt-4o", "model_mapping": "", "priority": 0, "weight": 1,
		"channel_ratio": "1", "max_concurrency": 0,
	})
	root := contractEngine(1, common.RoleRootUser)
	root.PUT("/api/channel/", UpdateChannel)
	updated := contractRequest(t, root, http.MethodPut, "/api/channel/", rename)
	require.Contains(t, updated.Body.String(), `"success":true`, updated.Body.String())

	var current model.Channel
	require.NoError(t, model.DB.First(&current, channel.Id).Error)
	assert.Equal(t, "native-contract-renamed", current.Name)
	assert.Equal(t, constant.ChannelTypeAIProxyLibrary, current.Type)
	assert.Equal(t, other, current.Other)
	assert.Equal(t, otherSettings, current.OtherSettings)
	assert.Nil(t, current.Setting)
}

// The console names every channel type the backend accepts; a new native type
// changes this fixture and fails the frontend catalog test until it is named.
func TestFrontendContractChannelTypeIds(t *testing.T) {
	ids := make([]int, 0, len(constant.ChannelTypeNames))
	for id := range constant.ChannelTypeNames {
		if id != constant.ChannelTypeUnknown {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	frontendFixture(t, "channel-type-ids.json", map[string]any{"ids": ids})
}

// The price fetch dialog lists the native presets and OpenRouter channels from
// this response; other channels are listed by the backend but not synced.
func TestFrontendContractSyncableChannels(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	for _, seed := range []struct {
		kind    int
		name    string
		baseURL string
	}{
		{constant.ChannelTypeOpenRouter, "contract-openrouter", "https://openrouter.ai/api"},
		{constant.ChannelTypeOpenAI, "contract-openai", "https://api.openai.com"},
	} {
		baseURL, priority, weight := seed.baseURL, int64(0), uint(0)
		channel := model.Channel{
			Type: seed.kind, Name: seed.name, Key: "sk-contract", Models: "gpt-4o",
			BaseURL: &baseURL, Priority: &priority, Weight: &weight, Status: common.ChannelStatusEnabled,
		}
		require.NoError(t, channel.Insert())
	}
	engine := contractEngine(1, common.RoleRootUser)
	engine.GET("/api/ratio_sync/channels", GetSyncableChannels)
	recorder := contractRequest(t, engine, http.MethodGet, "/api/ratio_sync/channels", nil)
	require.Equal(t, http.StatusOK, recorder.Code)
	body := decodeContractBody(t, recorder)
	require.Equal(t, true, body["success"])
	frontendFixture(t, "ratio-sync-channels.get.json", body)
}
