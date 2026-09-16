package router

import (
	"context"
	"fmt"
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
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayLegacyOperationsRejectIncompatibleChannels(t *testing.T) {
	for _, path := range []string{"/v1/completions", "/v1/moderations"} {
		for _, channelType := range []int{constant.ChannelTypeAnthropic, constant.ChannelTypeGemini, constant.ChannelTypeOllama, constant.ChannelTypeSub2API} {
			t.Run(fmt.Sprintf("%s/%d", path, channelType), func(t *testing.T) {
				var sends atomic.Int32
				url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1) })
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Update("type", channelType).Error)
				// Explicit allow cannot authorize a converter for another operation.
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Update("conversion_policy", "allow").Error)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				payload := `{"model":"gpt-4o-mini","prompt":"complete this"}`
				if path == "/v1/moderations" {
					payload = `{"model":"gpt-4o-mini","input":"inspect this"}`
				}
				status, body := gatewayLegacyOperationRequest(t, url, key, path, payload)
				assert.Equal(t, http.StatusServiceUnavailable, status, body)
				assert.Zero(t, sends.Load())
				var user model.User
				require.NoError(t, model.DB.First(&user, 1).Error)
				assert.Equal(t, 10000000, user.Quota)
				assert.Zero(t, user.UsedQuota)
				var recoveries, failures int64
				require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
				assert.Zero(t, recoveries)
				assert.Zero(t, failures)
			})
		}
	}
}

func TestGatewayLegacyOperationsPreserveNativeContracts(t *testing.T) {
	for _, test := range []struct {
		name, path, payload, response string
	}{
		{"completion_json", "/v1/completions", `{"model":"gpt-4o-mini","prompt":"complete this","max_tokens":7}`, `{"id":"cmpl_fixture","object":"text_completion","model":"gpt-4o-mini","choices":[{"index":0,"text":"answer","finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`},
		{"completion_sse", "/v1/completions", `{"model":"gpt-4o-mini","prompt":"complete this","max_tokens":7,"stream":true}`, "data: {\"id\":\"cmpl_fixture\",\"object\":\"text_completion\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"text\":\"answer\",\"finish_reason\":null}]}\n\ndata: {\"id\":\"cmpl_fixture\",\"object\":\"text_completion\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"text\":\"\",\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"total_tokens\":4}}\n\ndata: [DONE]\n\n"},
		{"moderation_json", "/v1/moderations", `{"model":"gpt-4o-mini","input":["inspect this"]}`, `{"id":"modr_fixture","model":"gpt-4o-mini","results":[{"flagged":false,"categories":{"violence":false},"category_scores":{"violence":0.01}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, test.path, r.URL.Path)
				assert.Equal(t, "Bearer local-fallback-key", r.Header.Get("Authorization"), "skip incapable higher-priority channel")
				var request map[string]any
				if assert.NoError(t, common.DecodeJson(r.Body, &request)) {
					assert.NotContains(t, request, "messages")
					if test.path == "/v1/completions" {
						assert.Equal(t, "complete this", request["prompt"])
						assert.EqualValues(t, 7, request["max_tokens"])
					} else {
						assert.Equal(t, []any{"inspect this"}, request["input"])
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(test.name, "sse") {
					w.Header().Set("Content-Type", "text/event-stream")
				}
				_, _ = io.WriteString(w, test.response)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("type", constant.ChannelTypeAnthropic).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			settings := model_setting.GetGlobalSettings()
			oldPolicy := settings.ChatCompletionsToResponsesPolicy
			t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = oldPolicy })
			settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{".*"}}
			status, body := gatewayLegacyOperationRequest(t, url, key, test.path, test.payload)
			assert.Equal(t, http.StatusOK, status, body)
			assert.EqualValues(t, 1, sends.Load())
			if strings.HasSuffix(test.name, "sse") {
				assert.Contains(t, body, `"text":"answer"`)
				assert.Contains(t, body, "data: [DONE]")
				assert.NotContains(t, body, `"delta"`)
			} else {
				var actual, expected map[string]any
				require.NoError(t, common.Unmarshal([]byte(body), &actual))
				require.NoError(t, common.Unmarshal([]byte(test.response), &expected))
				if test.path == "/v1/moderations" {
					// The existing billing contract exposes estimated usage when omitted upstream.
					usage, ok := actual["usage"].(map[string]any)
					require.True(t, ok)
					assert.Equal(t, "estimated", usage["usage_source"])
					delete(actual, "usage")
				}
				assert.Equal(t, expected, actual)
			}
			require.Eventually(t, func() bool {
				var recovery model.BillingRecovery
				return model.DB.First(&recovery).Error == nil && recovery.Status == model.BillingRecoveryStatusSettled && recovery.ConsumptionState == model.BillingRecoveryStateCompleted
			}, 2*time.Second, 10*time.Millisecond)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.Equal(t, 500, user.UsedQuota)
			assert.Equal(t, 9999500, user.Quota)
		})
	}
}

func gatewayLegacyOperationRequest(t *testing.T, url, key, path, payload string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, url+path, strings.NewReader(payload))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(body)
}

func TestGatewayCompletionNativeAdaptorPaths(t *testing.T) {
	for _, channel := range []struct {
		name string
		kind int
		path string
	}{
		{"DeepSeek", constant.ChannelTypeDeepSeek, "/beta/completions"},
		{"Ali", constant.ChannelTypeAli, "/compatible-mode/v1/completions"},
		{"Moonshot", constant.ChannelTypeMoonshot, "/v1/completions"},
		{"NewAPI", constant.ChannelTypeNewAPI, "/v1/completions"},
		{"OpenRouter", constant.ChannelTypeOpenRouter, "/v1/completions"},
		{"Xinference", constant.ChannelTypeXinference, "/v1/completions"},
	} {
		t.Run(channel.name, func(t *testing.T) {
			var sends atomic.Int32
			const responseBody = `{"id":"cmpl_fixture","object":"text_completion","model":"gpt-4o-mini","choices":[{"index":0,"text":"answer","finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, channel.path, r.URL.Path)
				var request map[string]any
				if assert.NoError(t, common.DecodeJson(r.Body, &request)) {
					assert.Equal(t, "complete this", request["prompt"])
					assert.EqualValues(t, 7, request["max_tokens"])
					assert.NotContains(t, request, "messages")
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, responseBody)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Update("type", channel.kind).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			status, body := gatewayLegacyOperationRequest(t, url, key, "/v1/completions", `{"model":"gpt-4o-mini","prompt":"complete this","max_tokens":7}`)
			require.Equal(t, http.StatusOK, status, body)
			assert.EqualValues(t, 1, sends.Load())
			assert.JSONEq(t, responseBody, body)
		})
	}
}
