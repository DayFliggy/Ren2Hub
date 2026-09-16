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

func TestGatewayTextReachesFourthAvailableCandidate(t *testing.T) {
	for _, test := range []struct{ name, model, path, body string }{
		{"chat", "gpt-4o-mini", "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`},
		{"responses", "gpt-4o-mini", "/v1/responses", `{"model":"gpt-4o-mini","input":"hello"}`},
		{"claude", "claude-3-5-sonnet", "/v1/messages", `{"model":"claude-3-5-sonnet","max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`},
		{"gemini", "gemini-2.5-flash", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				credentials := r.Header.Get("Authorization") + r.Header.Get("X-Api-Key") + r.Header.Get("X-Goog-Api-Key") + r.URL.Query().Get("key")
				assert.Contains(t, credentials, "candidate-4")
				gatewaySDKUpstream(w, r)
			})
			var base model.Channel
			require.NoError(t, model.DB.Where("models = ?", test.model).First(&base).Error)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("models = ?", test.model).Update("status", common.ChannelStatusManuallyDisabled).Error)
			var held []*service.Permit
			defer func() {
				for _, lease := range held {
					lease.Release()
				}
			}()
			for index := 1; index <= 4; index++ {
				channel := base
				channel.Id, channel.Name, channel.Key = 0, fmt.Sprintf("candidate-%d", index), fmt.Sprintf("candidate-%d", index)
				channel.Status, channel.CapacityTotal = common.ChannelStatusEnabled, 1
				channel.Priority = common.GetPointer(int64(5 - index))
				require.NoError(t, model.DB.Create(&channel).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
				if index < 4 {
					lease, _, err := service.AcquireGatewayChannelPermit(context.Background(), channel.Name, channel.Id, false, nil)
					require.NoError(t, err)
					held = append(held, lease)
				}
			}
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			var before model.User
			require.NoError(t, model.DB.Where("username = ?", "sdk-isolated").First(&before).Error)
			request, err := http.NewRequest(http.MethodPost, url+test.path, strings.NewReader(test.body))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+key)
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, response.StatusCode, string(body))
			assert.Contains(t, string(body), "hello")
			assert.EqualValues(t, 1, sends.Load())
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				var settled int64
				require.NoError(collect, model.DB.Model(&model.BillingRecovery{}).
					Where("status = ? AND consumption_state = ?", model.BillingRecoveryStatusSettled, model.BillingRecoveryStateCompleted).Count(&settled).Error)
				assert.EqualValues(collect, 1, settled)
			}, 2*time.Second, 10*time.Millisecond)
			var after model.User
			require.NoError(t, model.DB.First(&after, before.Id).Error)
			assert.Equal(t, before.RequestCount+1, after.RequestCount)
			assert.Greater(t, after.UsedQuota, before.UsedQuota)
			assert.Equal(t, after.UsedQuota-before.UsedQuota, before.Quota-after.Quota)
			var recoveries, logs, failures int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
			require.NoError(t, model.DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			assert.EqualValues(t, 1, recoveries)
			assert.EqualValues(t, 1, logs)
			assert.Zero(t, failures)
		})
	}
}
