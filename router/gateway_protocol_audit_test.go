package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayResponseTerminalsKeepRetryHealthAndUsageContracts(t *testing.T) {
	settings := model_setting.GetGlobalSettings()
	previous := settings.ChatCompletionsToResponsesPolicy
	settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{Enabled: true, AllChannels: true, ModelPatterns: []string{"^gpt-4o-mini$"}}
	t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = previous })
	for _, transport := range []string{"json", "buffered", "stream"} {
		for _, reason := range []string{"max_output_tokens", "content_filter", "future_reason"} {
			t.Run(transport+"/"+reason, func(t *testing.T) {
				var sends atomic.Int32
				base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					assert.Equal(t, "/v1/responses", r.URL.Path)
					body := `{"id":"resp_audit","status":"incomplete","incomplete_details":{"reason":"` + reason + `"},"output":[{"id":"msg_audit","type":"message","role":"assistant","content":[{"type":"output_text","text":"audit answer"}]}],"usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}`
					if transport == "json" {
						w.Header().Set("Content-Type", "application/json")
						_, _ = io.WriteString(w, body)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\ndata: \"response\":%s}\n\n", body)
				})
				events := captureGatewayRouteDecisions(t)
				requestBody := fmt.Sprintf(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}],"stream":%t}`, transport == "stream")
				result := <-startGatewayRouteRequest(t, base, key, requestBody)
				require.NoError(t, result.err)
				decision := awaitGatewayRouteDecision(t, events, result.requestID)
				require.Len(t, decision.Attempts, 1)
				assert.EqualValues(t, 1, sends.Load())
				if reason == "future_reason" {
					assert.Equal(t, 502, result.code, result.body)
					assert.Equal(t, service.AttemptOriginLocalConvert, decision.Attempts[0].Origin)
					assert.Equal(t, service.AttemptRetryForbidden, decision.Attempts[0].RetryPermission)
					assert.Equal(t, types.ResponseUnknown, decision.Attempts[0].Termination)
					assert.NotContains(t, result.body, "audit answer")
				} else {
					assert.Equal(t, 200, result.code, result.body)
					assert.Contains(t, result.body, "audit answer")
					assert.Equal(t, types.ResponseLimited, decision.Attempts[0].Termination)
					var logs []model.Log
					require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1)
					assert.Equal(t, 3, logs[0].PromptTokens)
					assert.Equal(t, 5, logs[0].CompletionTokens)
				}
				var health []model.ChannelHealth
				require.NoError(t, model.DB.Find(&health).Error)
				for _, row := range health {
					assert.Zero(t, row.FailureCount)
				}
				encoded, err := common.Marshal(decision)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "audit answer")
				assertGatewayRouteIdle(t, 1, 4)
			})
		}
	}
}

func TestGatewayUnsupportedToolImageNeverSendsOrDebits(t *testing.T) {
	var sends atomic.Int32
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	body := `{"model":"gpt-4o-mini","max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_a","name":"screen","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_a","content":[{"type":"image","source":{"type":"url","url":"https://never-fetch.example/image.png"}}]}]}]}`
	for _, policy := range []string{"safe", "strict", "allow"} {
		token, err := model.GetTokenByKey(key, true)
		require.NoError(t, err)
		token.ConversionPolicy = policy
		require.NoError(t, token.Update())
		status, response := gatewayStateHTTP(t, base, key, "/v1/messages", body)
		assert.Equal(t, 400, status, response)
		assert.False(t, strings.Contains(response, "never-fetch.example"))
	}
	assert.Zero(t, sends.Load())
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
	assert.Zero(t, count)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.EqualValues(t, 10000000, user.Quota)
	assert.Zero(t, user.UsedQuota)
}
