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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayTransportOwnershipHTTP(t *testing.T) {
	for _, header := range []string{"OpenAI-Project", "Host", "X-Custom-Tenant"} {
		t.Run(header, func(t *testing.T) {
			var sends atomic.Int64
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				gatewaySDKUpstream(w, r)
			})
			var channel model.Channel
			require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeOpenAI).First(&channel).Error)
			headerConfig, err := common.Marshal(map[string]string{
				header: "{client_header:X-State-Scope}", "X-Request-Id": "{client_header:X-Request-Id}", "Authorization": "Bearer {api_key}",
				"X-Stainless-Lang": "{client_header:X-SDK-Language}", "X-Goog-Api-Client": "{client_header:X-SDK-Language}",
			})
			require.NoError(t, err)
			require.NoError(t, model.DB.Model(&channel).Update("header_override", string(headerConfig)).Error)
			requestState := func(previous, scope, trace string) (int, string) {
				payload := fmt.Sprintf(`{"model":"gpt-4o-mini","input":"hello","previous_response_id":%q}`, previous)
				request, err := http.NewRequest(http.MethodPost, url+"/v1/responses", strings.NewReader(payload))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+key)
				request.Header.Set("X-State-Scope", scope)
				request.Header.Set("X-Request-Id", trace)
				request.Header.Set("X-SDK-Language", trace)
				response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				return response.StatusCode, string(body)
			}
			status, body := requestState("", "first.example.invalid", "trace-one")
			require.Equal(t, 200, status, body)
			status, body = requestState("resp_fixture", "first.example.invalid", "trace-two")
			require.Equal(t, 200, status, body)
			status, body = requestState("resp_fixture", "second.example.invalid", "trace-three")
			assert.Equal(t, 503, status, body)
			assert.EqualValues(t, 2, sends.Load(), "tenant/Host changes cannot dispatch bound state")
			var binding model.GatewayStateBinding
			require.NoError(t, model.DB.Where("kind = ?", "response").First(&binding).Error)
			assert.Len(t, binding.TransportID, 64)
			encoded, err := common.Marshal(binding)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "first.example.invalid")
			assert.NotContains(t, string(encoded), "local-test-key")
			require.NoError(t, model.DB.Model(&model.GatewayStateBinding{}).Where("channel_id = ?", channel.Id).Update("transport_id", "").Error)
			status, body = requestState("resp_fixture", "first.example.invalid", "trace-four")
			assert.Equal(t, 400, status, body)
			assert.EqualValues(t, 2, sends.Load(), "legacy bindings must establish a new conversation")
		})
	}
}

func TestGatewayFinalIdentityRejectionsDoNotSendOrDebit(t *testing.T) {
	for _, test := range []struct{ name, header, parameters string }{
		{"static_auth", `{"Authorization":"Bearer another-credential"}`, ""},
		{"runtime_auth", "", `{"operations":[{"mode":"set_header","path":"Authorization","value":"Bearer another-credential"}]}`},
		{"unknown_state", "", `{"previous_response_id":"unknown-after-override"}`},
		{"unknown_item", "", `{"input":[{"type":"item_reference","id":"unknown-after-override"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int64
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Updates(map[string]any{"header_override": test.header, "param_override": test.parameters}).Error)
			var before, after model.User
			require.NoError(t, model.DB.Where("username = ?", "sdk-isolated").First(&before).Error)
			status, body := gatewayStateHTTP(t, url, key, "/v1/responses", `{"model":"gpt-4o-mini","input":"hello"}`)
			assert.Equal(t, 400, status, body)
			assert.Zero(t, sends.Load())
			require.NoError(t, model.DB.First(&after, before.Id).Error)
			assert.EqualValues(t, before.Quota, after.Quota)
			var bills, unhealthy int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&bills).Error)
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&unhealthy).Error)
			assert.Zero(t, bills)
			assert.Zero(t, unhealthy)
		})
	}
}

func TestGatewayNativePromptReferenceDomainHTTP(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeOpenRouter} {
		for _, override := range []bool{false, true} {
			t.Run(fmt.Sprintf("type=%d/override=%t", channelType, override), func(t *testing.T) {
				var sends atomic.Int64
				url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
				updates := map[string]any{"type": channelType}
				body := `{"model":"gpt-4o-mini","input":"hello","prompt":{"id":"pmpt_local"}}`
				if override {
					updates["param_override"] = `{"prompt":{"id":"pmpt_local"}}`
					body = `{"model":"gpt-4o-mini","input":"hello"}`
				}
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Updates(updates).Error)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				status, response := gatewayStateHTTP(t, url, key, "/v1/responses", body)
				if channelType == constant.ChannelTypeOpenAI {
					assert.Equal(t, 200, status, response)
					assert.EqualValues(t, 1, sends.Load())
				} else {
					expected := 503 // Planning excluded every candidate before admission.
					if override {
						expected = 400 // An otherwise valid candidate injected the reference later.
					}
					assert.Equal(t, expected, status, response)
					assert.Zero(t, sends.Load())
				}
			})
		}
	}
}
