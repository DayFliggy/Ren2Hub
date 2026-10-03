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

func TestQueuedRequestRejectsChangedChannelIdentity(t *testing.T) {
	for _, change := range []string{"key", "endpoint", "disabled_key"} {
		t.Run(change, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				gatewaySDKUpstream(w, r)
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 4).Update("status", common.ChannelStatusManuallyDisabled).Error)
			channel, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			channel.CapacityTotal = 1
			if change == "disabled_key" {
				channel.ChannelInfo.IsMultiKey = true
				channel.ChannelInfo.MultiKeyStatusList = map[int]int{0: common.ChannelStatusEnabled}
			}
			require.NoError(t, channel.Update())
			require.NoError(t, service.RefreshChannelCapabilitiesByID(context.Background(), 1))
			holder, _, err := service.AcquireGatewayChannelPermit(context.Background(), "identity-holder", 1, false, service.NewGatewayWaitBudget(0))
			require.NoError(t, err)
			defer holder.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+key)
			type result struct {
				response *http.Response
				err      error
			}
			done := make(chan result, 1)
			go func() { response, err := http.DefaultClient.Do(req); done <- result{response, err} }()
			require.Eventually(t, func() bool { return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).Queued == 1 }, 3*time.Second, time.Millisecond)
			channel, err = model.GetChannelById(1, true)
			require.NoError(t, err)
			switch change {
			case "key":
				channel.Key = "replacement-fixture"
			case "endpoint":
				endpoint := *channel.BaseURL + "/changed"
				channel.BaseURL = &endpoint
			case "disabled_key":
				channel.ChannelInfo.MultiKeyStatusList[0] = common.ChannelStatusManuallyDisabled
			}
			require.NoError(t, channel.Update())
			require.NoError(t, service.RefreshChannelCapabilitiesByID(context.Background(), 1))
			holder.Release()
			got := <-done
			require.NoError(t, got.err)
			defer got.response.Body.Close()
			payload, err := io.ReadAll(got.response.Body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusServiceUnavailable, got.response.StatusCode, string(payload))
			assert.Zero(t, sends.Load())
			assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).InFlight)
			var records int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&records).Error)
			assert.Zero(t, records, "identity rejection happens before precharge")
		})
	}
}
