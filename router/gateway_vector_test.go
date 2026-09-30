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

func TestGatewayCloudflareUnimplementedRerankIsRejectedBeforeSend(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1) })
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Update("type", constant.ChannelCloudflare).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	request, err := http.NewRequest(http.MethodPost, url+"/v1/rerank", strings.NewReader(`{"model":"gpt-4o-mini","query":"hello","documents":["world"]}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode, string(body))
	assert.Zero(t, sends.Load())
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.EqualValues(t, 10000000, user.Quota)
	var recoveryCount int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveryCount).Error)
	assert.Zero(t, recoveryCount)
}

func TestGatewayVectorAccountingAndRequestBoundaries(t *testing.T) {
	for _, operation := range []struct {
		name, model, mappedModel, path, request, invalidRequest, response, empty, override string
	}{
		{"embedding", "text-embedding-3-small", "mapped-vector-deployment", "/v1/embeddings",
			`{"model":"text-embedding-3-small","input":["hello","world"]}`,
			`{"model":"text-embedding-3-small","input":null}`,
			`{"object":"list","data":[{"index":0,"embedding":[0,1]},{"index":1,"embedding":[1,0]}],"usage":{"prompt_tokens":10,"total_tokens":10}}`,
			`{"data":[]}`, `{"user":"gateway-override"}`},
		{"rerank", "rerank-v3.5", "mapped-rerank-deployment", "/v1/rerank",
			`{"model":"rerank-v3.5","query":"hello","documents":["first","second"],"top_n":2}`,
			`{"model":"rerank-v3.5","query":"","documents":["first"]}`,
			`{"results":[{"index":1,"relevance_score":0.9}],"usage":{"total_tokens":10}}`,
			`{"results":[]}`, `{"top_n":1}`},
	} {
		for _, scenario := range []struct {
			name, response string
			status         int
		}{
			{"valid_mapped_and_overridden", operation.response, http.StatusOK},
			{"empty_response", operation.empty, http.StatusBadGateway},
			{"negative_usage", strings.ReplaceAll(operation.response, ":10", ":-1"), http.StatusBadGateway},
			{"invalid_request", "", http.StatusBadRequest},
		} {
			t.Run(operation.name+"/"+scenario.name, func(t *testing.T) {
				var sends atomic.Int32
				url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					assert.Equal(t, operation.path, r.URL.Path)
					var request map[string]any
					if assert.NoError(t, common.DecodeJson(r.Body, &request)) {
						assert.Equal(t, operation.mappedModel, request["model"])
						if operation.name == "rerank" {
							assert.EqualValues(t, 1, request["top_n"])
						} else {
							assert.Equal(t, "gateway-override", request["user"])
						}
					}
					var user model.User
					if assert.NoError(t, model.DB.First(&user, 1).Error) {
						assert.EqualValues(t, 9999500, user.Quota, "reservation must precede the actual upstream request")
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, scenario.response)
				})
				channel, err := model.GetChannelById(1, true)
				require.NoError(t, err)
				channel.Models = operation.model
				mapping, err := common.Marshal(map[string]string{operation.model: operation.mappedModel})
				require.NoError(t, err)
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"models": channel.Models, "model_mapping": string(mapping), "param_override": operation.override, "setting": `{"pass_through_body_enabled":true}`}).Error)
				require.NoError(t, channel.UpdateAbilities(model.DB))
				prices, err := common.Marshal(map[string]float64{operation.model: 0.001, operation.mappedModel: 0.001})
				require.NoError(t, err)
				require.NoError(t, configureGatewayFixturePrices(string(prices), false))
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 1000000}).Error)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				requestBody := operation.request
				if scenario.name == "invalid_request" {
					requestBody = operation.invalidRequest
				}
				request, err := http.NewRequest(http.MethodPost, url+operation.path, strings.NewReader(requestBody))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+key)
				response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				assert.Equal(t, scenario.status, response.StatusCode, string(body))
				if scenario.name == "invalid_request" {
					assert.Zero(t, sends.Load())
				} else {
					assert.EqualValues(t, 1, sends.Load())
					require.Eventually(t, func() bool {
						var recovery model.BillingRecovery
						if model.DB.First(&recovery).Error != nil {
							return false
						}
						return model.BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) && scenario.status == http.StatusBadGateway || (recovery.Status == model.BillingRecoveryStatusSettled && recovery.ConsumptionState == model.BillingRecoveryStateCompleted)
					}, 2*time.Second, 10*time.Millisecond, "logical request must finish settlement after delivering the JSON response")
				}
				var user model.User
				var token model.Token
				require.NoError(t, model.DB.First(&user, 1).Error)
				require.NoError(t, model.DB.First(&token, 1).Error)
				charged := 0
				if scenario.status == http.StatusOK {
					charged = 500
				}
				assert.EqualValues(t, charged, user.UsedQuota)
				held := 0
				if scenario.status == http.StatusBadGateway {
					held = 500
				}
				assert.EqualValues(t, held, user.ReservedQuota)
				assert.EqualValues(t, 10000000-charged-held, user.Quota)
				assert.EqualValues(t, charged, token.UsedQuota)
				assert.EqualValues(t, 1000000-charged-held, token.RemainQuota)
				assert.EqualValues(t, held, token.ReservedQuota)
				var failures int64
				require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
				assert.Equal(t, scenario.status == http.StatusBadGateway, failures > 0)
				var recoveries []model.BillingRecovery
				require.NoError(t, model.DB.Find(&recoveries).Error)
				if scenario.name == "invalid_request" {
					assert.Empty(t, recoveries)
					return
				}
				require.Len(t, recoveries, 1)
				if scenario.status == http.StatusOK {
					assert.Equal(t, model.BillingRecoveryStatusSettled, recoveries[0].Status)
					assert.Equal(t, model.BillingRecoveryStateCompleted, recoveries[0].ConsumptionState)
				} else {
					assert.True(t, model.BillingRecoveryUsageUnconfirmed(recoveries[0].ConsumptionState))
				}
			})
		}
	}
}
