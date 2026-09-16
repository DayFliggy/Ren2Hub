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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayImageFinalQuantityAndNativeMapping(t *testing.T) {
	for _, test := range []struct {
		name, override string
		quota, status  int
	}{
		{"native_mapping", `{"n":3,"quality":"high"}`, 10000000, 200},
		{"invalid_override", `{"n":129}`, 10000000, 400},
		{"zero_override", `{"n":0}`, 10000000, 400},
		{"insufficient_final_quantity", `{"n":3}`, 1000, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				var request map[string]any
				if assert.NoError(t, common.DecodeJson(r.Body, &request)) {
					assert.Equal(t, "private-image-deployment", request["model"])
					assert.Equal(t, float64(3), request["n"])
					assert.Equal(t, "high", request["quality"])
					assert.Equal(t, false, request["vendor_flag"], "native extensions remain intact")
				}
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.Equal(t, test.quota-1500, user.Quota)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"b64_json":"first"},{"b64_json":"second"}]}`)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Updates(map[string]any{
				"setting": `{"pass_through_body_enabled":true}`, "param_override": test.override,
				"model_mapping": `{"gpt-4o-mini":"private-image-deployment"}`,
			}).Error)
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Update("quota", test.quota).Error)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "remain_quota": 100000}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			request, err := http.NewRequest(http.MethodPost, url+"/v1/images/generations", strings.NewReader(`{"model":"gpt-4o-mini","prompt":"draw a house","n":1,"vendor_flag":false}`))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, test.status, response.StatusCode, string(body))
			awaitGatewayPermitsReleased(t)
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			if test.status == 200 {
				assert.EqualValues(t, 1, sends.Load())
				assert.Equal(t, test.quota-1000, user.Quota)
				assert.Equal(t, 100000-1000, token.RemainQuota)
			} else {
				assert.Zero(t, sends.Load())
				assert.Equal(t, test.quota, user.Quota)
				assert.Equal(t, 100000, token.RemainQuota)
				var recoveries int64
				require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				assert.Zero(t, recoveries)
			}
			var failures int64
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			assert.Zero(t, failures)
		})
	}
}

// Exercise the public endpoint and durable settlement together. A bounded Token
// deliberately disables the trusted-quota shortcut in the shared SDK fixture.
func TestGatewayImageDeliveryAndSettlement(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		stream, interrupted     bool
		status, charge          int
	}{
		{"json", "application/json", `{"data":[{"b64_json":"first"},{"b64_json":"second"}]}`, false, false, 200, 1000},
		{"json_as_stream", "application/json", `{"data":[{"b64_json":"first"}]}`, true, false, 200, 500},
		{"empty_json", "application/json", `{"data":[]}`, false, true, 502, 0},
		{"malformed_json", "application/json", `{broken`, false, true, 502, 0},
		{"completed_stream", "text/event-stream", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"first\"}\n\n", true, false, 200, 500},
		{"partial_eof", "text/event-stream", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n", true, true, 200, 500},
		{"error_after_partial", "text/event-stream", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\ndata: {\"type\":\"error\",\"error\":{\"message\":\"upstream broke\"}}\n\n", true, true, 200, 500},
		{"negative_usage", "text/event-stream", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"image\",\"usage\":{\"output_tokens\":-3}}\n\n", true, true, 502, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "/v1/images/generations", r.URL.Path)
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.Equal(t, 10000000-1500, user.Quota, "reserve requested n=3 before the upstream receives it")
				}
				w.Header().Set("Content-Type", test.contentType)
				_, _ = io.WriteString(w, test.body)
			})
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "remain_quota": 100000}).Error)
			request, err := http.NewRequest(http.MethodPost, url+"/v1/images/generations", strings.NewReader(fmt.Sprintf(`{"model":"gpt-4o-mini","prompt":"draw a house","n":3,"stream":%v}`, test.stream)))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, test.status, response.StatusCode, string(body))
			if test.stream && test.status == 200 {
				if test.interrupted {
					assert.Equal(t, 1, strings.Count(string(body), `"code":"stream_interrupted"`))
					assert.NotContains(t, string(body), "data: [DONE]")
				} else {
					assert.Equal(t, 1, strings.Count(string(body), "data: [DONE]"))
				}
			}
			awaitGatewayPermitsReleased(t)
			assert.EqualValues(t, 1, sends.Load(), "accepted image generation cannot replay")
			var user model.User
			var token model.Token
			var logs []model.Log
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			assert.Equal(t, 10000000-test.charge, user.Quota)
			assert.Equal(t, 100000-test.charge, token.RemainQuota)
			assert.Equal(t, test.charge, user.UsedQuota)
			if test.charge == 0 {
				assert.Empty(t, logs)
				assert.Zero(t, user.RequestCount)
			} else {
				require.Len(t, logs, 1)
				assert.Equal(t, test.charge, logs[0].Quota)
				assert.Equal(t, 1, user.RequestCount)
			}
			var failures int64
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			if test.interrupted {
				assert.Positive(t, failures)
			} else {
				assert.Zero(t, failures)
			}
		})
	}
}
