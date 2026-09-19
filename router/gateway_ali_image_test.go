package router

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayAliAsyncImageAcceptance(t *testing.T) {
	for _, scenario := range []string{"success", "poll_failed", "missing_task_id", "unknown_submit", "rejected_then_success"} {
		t.Run(scenario, func(t *testing.T) {
			var creates, polls atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				assert.Equal(t, "tenant-fixture", r.Header.Get("X-Tenant"))
				if r.Method == http.MethodPost {
					n := creates.Add(1)
					assert.True(t, strings.HasSuffix(r.URL.Path, "/image-synthesis"))
					if scenario == "unknown_submit" || scenario == "rejected_then_success" && n == 1 {
						status := http.StatusBadGateway
						if scenario == "rejected_then_success" {
							status = http.StatusTooManyRequests
						}
						w.WriteHeader(status)
						_, _ = io.WriteString(w, `{"code":"Throttling.RateQuota","message":"submit failed"}`)
						return
					}
					if scenario == "missing_task_id" {
						_, _ = io.WriteString(w, `{"output":{"task_status":"PENDING"}}`)
						return
					}
					_, _ = io.WriteString(w, `{"output":{"task_id":"task-local","task_status":"PENDING"}}`)
					return
				}
				polls.Add(1)
				assert.Equal(t, "/api/v1/tasks/task-local", r.URL.Path)
				assert.Empty(t, r.Header.Get("X-DashScope-Async"))
				if scenario == "poll_failed" {
					w.WriteHeader(http.StatusServiceUnavailable)
					_, _ = io.WriteString(w, `{"error":{"message":"poll unavailable","type":"server_error"}}`)
					return
				}
				_, _ = io.WriteString(w, `{"output":{"task_id":"task-local","task_status":"SUCCEEDED","results":[{"url":"https://example.invalid/result.png"}]}}`)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", "gpt-4o-mini").Updates(map[string]any{
				"type": constant.ChannelTypeAli, "header_override": `{"X-Tenant":"tenant-fixture"}`,
			}).Error)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 2000}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			status, output := postGatewayTask(t, url, key, "/v1/images/generations", `{"model":"gpt-4o-mini","prompt":"image fixture","n":1,"response_format":"url"}`)
			success := scenario == "success" || scenario == "rejected_then_success"
			if success {
				assert.Equal(t, http.StatusOK, status, output)
				assert.Contains(t, output, "https://example.invalid/result.png")
			} else {
				assert.GreaterOrEqual(t, status, 500, output)
				assert.NotContains(t, output, "task-local")
			}
			wantCreates, wantPolls, charge := 1, 0, 0
			if success || scenario == "poll_failed" {
				wantPolls = 1
			}
			if scenario == "rejected_then_success" {
				wantCreates = 2
			}
			if success {
				charge = 500
			}
			assert.EqualValues(t, wantCreates, creates.Load(), "accepted or unknown creation must not replay after failure")
			assert.EqualValues(t, wantPolls, polls.Load())
			awaitGatewayPermitsReleased(t)
			var user model.User
			var token model.Token
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.DB.First(&token, 1).Error)
			assert.EqualValues(t, 10000000-500, user.Quota, "accepted or unknown creation retains its reservation")
			assert.EqualValues(t, 2000-500, token.RemainQuota)
			assert.EqualValues(t, charge, user.UsedQuota)
			assert.EqualValues(t, charge, token.UsedQuota)
			assert.EqualValues(t, 500-charge, token.ReservedQuota)
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			if success {
				assert.Len(t, logs, 1)
			} else {
				assert.Empty(t, logs)
				for range 2 {
					var recovery model.BillingRecovery
					require.NoError(t, model.DB.First(&recovery).Error)
					assert.Equal(t, model.BillingRecoveryStatusActive, recovery.Status)
					assert.Equal(t, model.BillingRecoveryStateUsagePending, recovery.ConsumptionState)
					_, err := service.RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+3600, 10)
					require.NoError(t, err)
				}
			}
		})
	}
}
