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

func TestGatewayPhaseDeadlineReleasesAdmissionAndHoldsReservation(t *testing.T) {
	for _, phase := range []string{"headers", "non_stream_body", "stream_heartbeats"} {
		t.Run(phase, func(t *testing.T) {
			oldHeader, oldBody, oldFirst := common.RelayResponseHeaderTimeout, common.RelayNonStreamTimeout, common.StreamingFirstOutputTimeout
			common.RelayResponseHeaderTimeout, common.RelayNonStreamTimeout, common.StreamingFirstOutputTimeout = 4, 4, 1
			if phase == "headers" {
				common.RelayResponseHeaderTimeout = 1
			} else if phase == "non_stream_body" {
				common.RelayNonStreamTimeout = 1
			}
			t.Cleanup(func() {
				common.RelayResponseHeaderTimeout, common.RelayNonStreamTimeout, common.StreamingFirstOutputTimeout = oldHeader, oldBody, oldFirst
				service.InitHttpClient()
			})
			var sends, cancellations atomic.Int32
			cleanup := make(chan struct{})
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				sends.Add(1)
				assert.EqualValues(t, 1, service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(1)).InFlight)
				assert.EqualValues(t, 1, service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).InFlight)
				if phase == "non_stream_body" {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"choices":[`)
					w.(http.Flusher).Flush()
				}
				if phase == "stream_heartbeats" {
					w.Header().Set("Content-Type", "text/event-stream")
					w.(http.Flusher).Flush()
					ticker := time.NewTicker(50 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-cleanup:
							return
						case <-r.Context().Done():
							cancellations.Add(1)
							return
						case <-ticker.C:
							_, _ = io.WriteString(w, ": waiting\n\n")
							w.(http.Flusher).Flush()
						}
					}
				}
				select {
				case <-cleanup:
				case <-r.Context().Done():
					cancellations.Add(1)
				}
			})
			t.Cleanup(func() { close(cleanup) })
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 4).Update("status", common.ChannelStatusManuallyDisabled).Error)
			require.NoError(t, service.RefreshChannelCapabilitiesByID(context.Background(), 4))
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 2000}).Error)
			body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"deadline fixture"}],"stream":false}`
			if phase == "stream_heartbeats" {
				body = strings.Replace(body, `"stream":false`, `"stream":true`, 1)
			}
			req, err := http.NewRequest(http.MethodPost, url+"/v1/chat/completions", strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
			require.NoError(t, err, "the gateway must finish before the client deadline")
			defer response.Body.Close()
			payload, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, response.StatusCode, http.StatusBadRequest, string(payload))
			assert.NotContains(t, string(payload), ": waiting")
			assert.EqualValues(t, 1, sends.Load())
			require.Eventually(t, func() bool { return cancellations.Load() == sends.Load() }, time.Second, time.Millisecond)
			assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(1)).InFlight)
			assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).InFlight)
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			assert.EqualValues(t, 9999500, user.Quota)
			assert.EqualValues(t, 500, user.ReservedQuota)
			assert.EqualValues(t, 1500, token.RemainQuota)
			assert.EqualValues(t, 500, token.ReservedQuota)
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery).Error)
			assert.True(t, model.BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState))
		})
	}
}
