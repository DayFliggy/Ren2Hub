package router

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayLegacyEmbeddingAliases(t *testing.T) {
	for _, provider := range []struct {
		name   string
		typeID int
		path   string
	}{
		{"OpenAI", constant.ChannelTypeOpenAI, "/v1/embeddings"},
		{"Azure", constant.ChannelTypeAzure, "/openai/deployments/private-embedding/embeddings"},
	} {
		t.Run(provider.name, func(t *testing.T) {
			for _, test := range []struct{ name, path, body string }{
				{"standard", "/v1/embeddings", `{"model":"gpt-4o-mini","input":["alpha","beta"],"encoding_format":"float"}`},
				{"path_model_only", "/v1/engines/gpt-4o-mini/embeddings", `{"input":["alpha","beta"],"encoding_format":"float"}`},
				{"same_body_model", "/v1/engines/gpt-4o-mini/embeddings", `{"model":"gpt-4o-mini","input":["alpha","beta"],"encoding_format":"float"}`},
			} {
				t.Run(test.name, func(t *testing.T) {
					var sends atomic.Int32
					gateway, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
						sends.Add(1)
						assert.Equal(t, provider.path, r.URL.Path)
						var request map[string]any
						if assert.NoError(t, common.DecodeJson(r.Body, &request)) {
							assert.Equal(t, "private-embedding", request["model"])
							assert.Equal(t, []any{"alpha", "beta"}, request["input"])
							assert.Equal(t, "float", request["encoding_format"])
							assert.NotContains(t, request, "contents")
						}
						if provider.typeID == constant.ChannelTypeAzure {
							assert.Equal(t, "2025-04-01-preview", r.URL.Query().Get("api-version"))
							assert.Equal(t, "local-test-key", r.Header.Get("api-key"))
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, `{"object":"list","model":"private-embedding","data":[{"object":"embedding","index":0,"embedding":[1,0]},{"object":"embedding","index":1,"embedding":[0,1]}],"usage":{"prompt_tokens":4,"total_tokens":4}}`)
					})
					require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Updates(map[string]any{
						"type": provider.typeID, "model_mapping": `{"gpt-4o-mini":"private-embedding"}`, "other": "2025-04-01-preview",
					}).Error)
					require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{
						"unlimited_quota": false, "remain_quota": 100000, "model_limits_enabled": true, "model_limits": "gpt-4o-mini",
					}).Error)
					require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
					status, body := gatewayLegacyEndpointRequest(t, gateway, key, test.path, test.body)
					require.Equal(t, http.StatusOK, status, body)
					var response struct {
						Object string `json:"object"`
						Data   []struct {
							Index     int       `json:"index"`
							Embedding []float64 `json:"embedding"`
						} `json:"data"`
					}
					require.NoError(t, common.UnmarshalJsonStr(body, &response))
					assert.Equal(t, "list", response.Object)
					require.Len(t, response.Data, 2)
					assert.Equal(t, []float64{1, 0}, response.Data[0].Embedding)
					assert.Equal(t, []float64{0, 1}, response.Data[1].Embedding)
					assert.EqualValues(t, 1, sends.Load())
					awaitGatewayPermitsReleased(t)
					var user model.User
					var token model.Token
					require.NoError(t, model.DB.First(&user, 1).Error)
					require.NoError(t, model.DB.First(&token, 1).Error)
					assert.Equal(t, 10000000-500, user.Quota)
					assert.Equal(t, 100000-500, token.RemainQuota)
					var recoveries, logs int64
					require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
					require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
					assert.EqualValues(t, 1, recoveries)
					assert.EqualValues(t, 1, logs)
				})
			}
		})
	}
}

func TestGatewayLegacyEndpointRejections(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		status           int
		noAuth           bool
	}{
		{"legacy_text_edits", "/v1/edits", `{"model":"gpt-4o-mini","input":"hello","instruction":"fix spelling"}`, 501, false},
		{"edits_missing_model", "/v1/edits", `{}`, 501, false},
		{"edits_authentication", "/v1/edits", `{}`, 401, true},
		{"standard_authentication", "/v1/embeddings", `{"model":"gpt-4o-mini","input":"hello"}`, 401, true},
		{"alias_authentication", "/v1/engines/gpt-4o-mini/embeddings", `{"input":"hello"}`, 401, true},
		{"standard_missing_model", "/v1/embeddings", `{"input":"hello"}`, 400, false},
		{"standard_model_forbidden", "/v1/embeddings", `{"model":"claude-3-5-sonnet","input":"hello"}`, 403, false},
		{"alias_model_forbidden", "/v1/engines/claude-3-5-sonnet/embeddings", `{"input":"hello"}`, 403, false},
		{"allowed_path_forbidden_body", "/v1/engines/gpt-4o-mini/embeddings", `{"model":"claude-3-5-sonnet","input":"hello"}`, 400, false},
		{"forbidden_path_allowed_body", "/v1/engines/claude-3-5-sonnet/embeddings", `{"model":"gpt-4o-mini","input":"hello"}`, 400, false},
		{"standard_missing_input", "/v1/embeddings", `{"model":"gpt-4o-mini"}`, 400, false},
		{"alias_missing_input", "/v1/engines/gpt-4o-mini/embeddings", `{}`, 400, false},
		{"alias_nonstring_model", "/v1/engines/gpt-4o-mini/embeddings", `{"model":1,"input":"hello"}`, 400, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			gateway, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewayMediaSDKUpstream(w, r) })
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{
				"unlimited_quota": false, "remain_quota": 100000, "model_limits_enabled": true, "model_limits": "gpt-4o-mini",
			}).Error)
			if test.noAuth {
				key = ""
			}
			status, body := gatewayLegacyEndpointRequest(t, gateway, key, test.path, test.body)
			assert.Equal(t, test.status, status, body)
			if test.status == http.StatusNotImplemented {
				assert.Contains(t, body, "api_not_implemented")
			}
			awaitGatewayPermitsReleased(t)
			assert.Zero(t, sends.Load())
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			assert.Equal(t, 10000000, user.Quota)
			assert.Equal(t, 100000, token.RemainQuota)
			var recoveries, logs, failures int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
			require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			assert.Zero(t, recoveries)
			assert.Zero(t, logs)
			assert.Zero(t, failures)
		})
	}
}

func gatewayLegacyEndpointRequest(t *testing.T, gateway, key, path, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, gateway+path, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(data)
}
