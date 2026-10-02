package router

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayNamelessToolFailsWithoutReplayingAcceptedWork(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%t", stream), func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "/v1/chat/completions", r.URL.Path)
				if !stream {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"id":"invalid-tool","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"partial valid output","tool_calls":[{"id":"call_1","type":"function","function":{"arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"id\":\"invalid-tool\",\"model\":\"gpt-4o-mini\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial valid output\"}}]}\n\n")
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, "data: {\"id\":\"invalid-tool\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}}\n\ndata: [DONE]\n\n")
			})
			for _, id := range []int{1, 4} {
				channel, err := model.GetChannelById(id, true)
				require.NoError(t, err)
				channel.Type = constant.ChannelTypeAdvancedCustom
				channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{IncomingPath: "/v1/responses", UpstreamPath: "/v1/chat/completions", Converter: "openai_responses_to_openai_chat_completions"}}}})
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "settings": channel.OtherSettings}).Error)
			}
			require.NoError(t, service.InitRouteCapabilityIndex(t.Context()))
			events := captureGatewayRouteDecisions(t)
			request, err := http.NewRequest(http.MethodPost, base+"/v1/responses", strings.NewReader(fmt.Sprintf(`{"model":"gpt-4o-mini","input":"test","max_output_tokens":32,"stream":%t}`, stream)))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+key)
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			payload, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			decision := awaitGatewayRouteDecision(t, events, response.Header.Get(common.RequestIdKey))
			require.Len(t, decision.Attempts, 1)
			assert.Equal(t, service.AttemptOriginLocalConvert, decision.Attempts[0].Origin)
			assert.Equal(t, service.AttemptRetryForbidden, decision.Attempts[0].RetryPermission)
			assert.EqualValues(t, 1, sends.Load())
			assert.NotContains(t, string(payload), "response.completed")
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery).Error)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.Zero(t, user.ReservedQuota)
			if stream {
				assert.Equal(t, http.StatusOK, response.StatusCode)
				assert.Contains(t, string(payload), "partial valid output")
				assert.Contains(t, string(payload), "error")
				assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
				assert.EqualValues(t, 500, user.UsedQuota)
			} else {
				assert.Equal(t, http.StatusBadGateway, response.StatusCode)
				assert.NotContains(t, string(payload), "partial valid output")
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
				assert.Zero(t, user.UsedQuota)
			}
		})
	}
}
