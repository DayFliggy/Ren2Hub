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
	"github.com/tidwall/gjson"
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
					assert.EqualValues(t, test.quota-1500, user.Quota)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"b64_json":"first"},{"b64_json":"second"}]}`)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Updates(map[string]any{
				"setting": `{"pass_through_body_enabled":true}`, "param_override": test.override,
				"model_mapping": `{"gpt-4o-mini":"private-image-deployment"}`,
			}).Error)
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Update("available_quota", test.quota).Error)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 100000}).Error)
			require.NoError(t, configureGatewayImagePrice("gpt-4o-mini"))
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
				assert.EqualValues(t, test.quota-1000, user.Quota)
				assert.EqualValues(t, 100000-1000, token.RemainQuota)
			} else {
				assert.Zero(t, sends.Load())
				assert.EqualValues(t, test.quota, user.Quota)
				assert.EqualValues(t, 100000, token.RemainQuota)
				var recoveries int64
				require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&recoveries).Error)
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
	// The n=3 reservation is 1500. Delivered output whose image count cannot be
	// confirmed settles at that frozen estimate; failures without output release it.
	for _, test := range []struct {
		name, contentType, body     string
		stream, interrupted, frozen bool
		status, charge              int
	}{
		{"json", "application/json", `{"data":[{"b64_json":"first"},{"b64_json":"second"}]}`, false, false, false, 200, 1000},
		{"json_as_stream", "application/json", `{"data":[{"b64_json":"first"}]}`, true, false, false, 200, 500},
		{"empty_json", "application/json", `{"data":[]}`, false, true, false, 502, 0},
		{"malformed_json", "application/json", `{broken`, false, true, false, 502, 0},
		{"completed_stream", "text/event-stream", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"first\"}\n\n", true, false, false, 200, 500},
		{"partial_eof", "text/event-stream", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n", true, true, true, 200, 1500},
		{"error_after_partial", "text/event-stream", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\ndata: {\"type\":\"error\",\"error\":{\"message\":\"upstream broke\"}}\n\n", true, true, true, 200, 1500},
		{"negative_usage", "text/event-stream", "data: {\"type\":\"image_generation.completed\",\"b64_json\":\"image\",\"usage\":{\"output_tokens\":-3}}\n\n", true, true, false, 502, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "/v1/images/generations", r.URL.Path)
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.EqualValues(t, 10000000-1500, user.Quota, "reserve requested n=3 before the upstream receives it")
				}
				w.Header().Set("Content-Type", test.contentType)
				_, _ = io.WriteString(w, test.body)
			})
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 100000}).Error)
			require.NoError(t, configureGatewayImagePrice("gpt-4o-mini"))
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
			var recovery model.BillingRecovery
			var logs []model.Log
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			require.NoError(t, model.DB.First(&recovery).Error)
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			assert.Zero(t, user.ReservedQuota)
			assert.Zero(t, token.ReservedQuota)
			assert.EqualValues(t, 10000000-test.charge, user.Quota)
			assert.EqualValues(t, 100000-test.charge, token.RemainQuota)
			assert.EqualValues(t, test.charge, user.UsedQuota)
			if test.charge == 0 {
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status, "no image reached the client")
				assert.Empty(t, logs)
				assert.Zero(t, user.RequestCount)
			} else {
				assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
				require.Len(t, logs, 1)
				assert.EqualValues(t, test.charge, logs[0].Quota)
				assert.Equal(t, 1, user.RequestCount)
				assert.Equal(t, test.frozen, gjson.Get(logs[0].Other, "admin_info.usage_billing_path").String() == "frozen_estimate")
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

func TestGatewayImageExpressionUsesActualCountAndSettlesInterruptedAtEstimate(t *testing.T) {
	for _, test := range []struct {
		name, contentType, body string
		charge                  int64
	}{
		{"completed", "application/json", `{"data":[{"b64_json":"first"},{"b64_json":"second"}]}`, 1000},
		// The overridden n=3 estimate is the only price evidence for a partial image.
		{"partial", "text/event-stream", "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"partial\"}\n\n", 1500},
	} {
		t.Run(test.name, func(t *testing.T) {
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				var user model.User
				require.NoError(t, model.DB.First(&user, 1).Error)
				assert.EqualValues(t, 1500, user.ReservedQuota, "final overridden n=3 is reserved before send")
				w.Header().Set("Content-Type", test.contentType)
				_, _ = io.WriteString(w, test.body)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Update("param_override", `{"n":3}`).Error)
			_, err := model.GetActiveBillingPrice(t.Context(), "gpt-4o-mini")
			require.NoError(t, err)
			_, err = setGatewayExpression("gpt-4o-mini", `v1:tier("images", fixed(0.001)) * image_count`)
			require.NoError(t, err)
			request, err := http.NewRequest(http.MethodPost, url+"/v1/images/generations", strings.NewReader(`{"model":"gpt-4o-mini","prompt":"draw","n":1}`))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			_, err = io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, response.StatusCode)
			awaitGatewayPermitsReleased(t)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.EqualValues(t, test.charge, user.UsedQuota)
			assert.Zero(t, user.ReservedQuota)
			var log model.Log
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).First(&log).Error)
			if test.name == "partial" {
				assert.Equal(t, "frozen_estimate", gjson.Get(log.Other, "admin_info.usage_billing_path").String())
				return
			}
			assert.Contains(t, log.Other, `"image_count":2`)
		})
	}
}
