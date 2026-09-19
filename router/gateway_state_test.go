package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayStateContinuationHTTP(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			calls := make(chan string, 8)
			url, tokenKey, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, request *http.Request) {
				calls <- strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
				gatewaySDKUpstream(w, request)
			})
			var channel model.Channel
			require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeOpenAI).Order("id").First(&channel).Error)
			channel.Key = "state-key-one\nstate-key-two"
			channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}
			require.NoError(t, model.DB.Model(&channel).Updates(map[string]any{"key": channel.Key, "channel_info": channel.ChannelInfo}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			client := &http.Client{Timeout: 5 * time.Second}
			requestState := func(previous string) (int, string) {
				payload := map[string]any{"model": "gpt-4o-mini", "input": "hello", "stream": stream}
				if previous != "" {
					payload["previous_response_id"] = previous
				}
				body, err := common.Marshal(payload)
				require.NoError(t, err)
				request, err := http.NewRequest(http.MethodPost, url+"/v1/responses", strings.NewReader(string(body)))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+tokenKey)
				response, err := client.Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				return response.StatusCode, string(data)
			}
			status, body := requestState("")
			require.Equal(t, http.StatusOK, status, body)
			require.Contains(t, body, "resp_fixture")
			selected := <-calls
			other := "state-key-one"
			if selected == other {
				other = "state-key-two"
			}
			var binding model.GatewayStateBinding
			require.NoError(t, model.DB.Where("digest = ? AND kind = ?", model.GatewayDigest("resp_fixture"), "response").First(&binding).Error)
			channel.Key = other + "\n" + selected
			require.NoError(t, model.DB.Model(&channel).Update("key", channel.Key).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			status, body = requestState("resp_fixture")
			require.Equal(t, http.StatusOK, status, body)
			assert.Equal(t, selected, <-calls)
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				var settled int64
				if assert.NoError(collect, model.DB.Model(&model.BillingRecovery{}).Where("token_id = ? AND status = ?", binding.TokenID, model.BillingRecoveryStatusSettled).Count(&settled).Error) {
					assert.EqualValues(collect, 2, settled)
				}
			}, 2*time.Second, 5*time.Millisecond, "wait for both successful HTTP responses to finish settlement")

			var user model.User
			require.NoError(t, model.DB.First(&user, binding.UserID).Error)
			quota := user.Quota
			channel.ChannelInfo.MultiKeyStatusList = map[int]int{1: common.ChannelStatusManuallyDisabled}
			require.NoError(t, model.DB.Model(&channel).Update("channel_info", channel.ChannelInfo).Error)
			status, _ = requestState("resp_fixture")
			assert.Equal(t, http.StatusServiceUnavailable, status)
			assert.Empty(t, calls, "a disabled owner cannot switch to another key")

			channel.ChannelInfo.MultiKeyStatusList = nil
			require.NoError(t, model.DB.Model(&channel).Updates(map[string]any{"channel_info": channel.ChannelInfo, "base_url": *channel.BaseURL + "/changed"}).Error)
			status, _ = requestState("resp_fixture")
			assert.Equal(t, http.StatusServiceUnavailable, status)
			assert.Empty(t, calls, "an endpoint identity change cannot reuse the state")
			require.NoError(t, model.DB.Model(&binding).Update("expires_at", time.Now().Add(-time.Second).Unix()).Error)
			status, _ = requestState("resp_fixture")
			assert.Equal(t, http.StatusBadRequest, status)
			assert.Empty(t, calls)
			require.NoError(t, model.DB.First(&user, binding.UserID).Error)
			assert.EqualValues(t, quota, user.Quota, "rejected state continuations never debit")
		})
	}
}
