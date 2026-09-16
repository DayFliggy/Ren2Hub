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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayMediaCredentialLimitScansIndependentChannelBeforeSending(t *testing.T) {
	for _, protocol := range []string{"midjourney", "video"} {
		t.Run(protocol, func(t *testing.T) {
			var sends atomic.Int32
			upstream := func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				if protocol == "midjourney" {
					assert.Equal(t, "local-fallback-key", r.Header.Get("mj-api-secret"))
					_, _ = io.WriteString(w, `{"code":1,"result":"fallback-job"}`)
				} else {
					assert.Equal(t, "Bearer local-fallback-key", r.Header.Get("Authorization"))
					_, _ = io.WriteString(w, `{"id":"fallback-job","status":"queued"}`)
				}
			}
			setup := setupGatewayMidjourney
			path, body := "/mj/submit/imagine", `{"prompt":"fixture"}`
			if protocol == "video" {
				setup = setupGatewayTask
				path, body = "/v1/videos", `{"model":"sora-2","prompt":"fixture"}`
			}
			url, key, _ := setup(t, upstream)
			credential, err := model.EnsureGatewayCredential(context.Background(), 1, "local-test-key")
			require.NoError(t, err)
			config := model.DefaultGatewayConfig()
			config.CredentialRates[credential.ID] = model.GatewayProviderRateLimit{RPM: 1}
			encoded, err := common.Marshal(config)
			require.NoError(t, err)
			require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
			// The channel-level advisory read cannot see this credential quota.
			// Only the final atomic reservation rejects A, before any real send.
			reservation, err := service.ReserveGatewayProviderRate(context.Background(), common.RDB,
				[]service.GatewayProviderRateScope{{ID: service.GatewayCredentialRateScopeID(credential.ID), RPM: 1}}, 0)
			require.NoError(t, err)
			require.NoError(t, service.FinishGatewayProviderRate(context.Background(), common.RDB, reservation, 0, true, true, 0))
			code, response := postGatewayTask(t, url, key, path, body)
			require.Equal(t, http.StatusOK, code, response)
			assert.EqualValues(t, 1, sends.Load())
			var billings, failures int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&billings).Error)
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			assert.EqualValues(t, 1, billings)
			assert.Zero(t, failures)
			for _, channelID := range []int{1, 4} {
				assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(channelID)).InFlight)
			}
		})
	}
}

func TestGatewayPreSendRejectionStillQueuesPreviouslyBusyChannel(t *testing.T) {
	for _, protocol := range []string{"text", "midjourney", "video", "text_capacity_expansion"} {
		t.Run(protocol, func(t *testing.T) {
			var sends atomic.Int32
			upstream := func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				if protocol == "midjourney" {
					assert.Equal(t, "local-test-key", r.Header.Get("mj-api-secret"))
					_, _ = io.WriteString(w, `{"code":1,"result":"queued-on-A"}`)
					return
				}
				assert.Equal(t, "Bearer local-test-key", r.Header.Get("Authorization"))
				if protocol == "video" {
					_, _ = io.WriteString(w, `{"id":"queued-on-A","status":"queued"}`)
					return
				}
				gatewaySDKUpstream(w, r)
			}
			setup := setupGatewaySDKWithUpstream
			path, body := "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`
			switch protocol {
			case "midjourney":
				setup = setupGatewayMidjourney
				path, body = "/mj/submit/imagine", `{"prompt":"fixture"}`
			case "video":
				setup = setupGatewayTask
				path, body = "/v1/videos", `{"model":"sora-2","prompt":"fixture"}`
			}
			url, key, _ := setup(t, upstream)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("capacity_total", 1).Error)
			credential, err := model.EnsureGatewayCredential(context.Background(), 4, "local-fallback-key")
			require.NoError(t, err)
			config := model.DefaultGatewayConfig()
			config.CredentialRates[credential.ID] = model.GatewayProviderRateLimit{RPM: 1}
			encoded, err := common.Marshal(config)
			require.NoError(t, err)
			require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
			reservation, err := service.ReserveGatewayProviderRate(context.Background(), common.RDB,
				[]service.GatewayProviderRateScope{{ID: service.GatewayCredentialRateScopeID(credential.ID), RPM: 1}}, 0)
			require.NoError(t, err)
			require.NoError(t, service.FinishGatewayProviderRate(context.Background(), common.RDB, reservation, 0, true, true, 0))
			holder, _, err := service.AcquireGatewayChannelPermit(context.Background(), "existing-A", 1, false, service.NewGatewayWaitBudget(0))
			require.NoError(t, err)
			defer holder.Release()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+path, strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+key)
			type requestResult struct {
				response *http.Response
				err      error
			}
			done := make(chan requestResult, 1)
			go func() {
				response, err := http.DefaultClient.Do(req)
				done <- requestResult{response, err}
			}()
			require.Eventually(t, func() bool {
				return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).Queued == 1
			}, time.Second, time.Millisecond, "B's final quota rejection must not skip A's eligible queue")
			assert.Zero(t, sends.Load())
			if protocol == "text_capacity_expansion" {
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("capacity_total", 2).Error)
				model.NotifyChannelCapabilityChanged(1)
			} else {
				holder.Release()
			}
			result := <-done
			require.NoError(t, result.err)
			defer result.response.Body.Close()
			payload, err := io.ReadAll(result.response.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, result.response.StatusCode, string(payload))
			assert.EqualValues(t, 1, sends.Load())
			if protocol == "text_capacity_expansion" {
				require.Eventually(t, func() bool {
					return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).InFlight == 1
				}, time.Second, time.Millisecond, "expansion wakes the waiter without releasing the old holder")
			}
			var billings int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&billings).Error)
			assert.EqualValues(t, 1, billings)
		})
	}
}
