package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
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

func TestGatewayToolImageHoistPreservesResultAndSettlesOnce(t *testing.T) {
	for _, policy := range []string{"safe", "strict", "allow"} {
		t.Run(policy, func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				payload, err := io.ReadAll(r.Body)
				if !assert.NoError(t, err) {
					http.Error(w, "cannot inspect request", http.StatusBadRequest)
					return
				}
				var request dto.GeneralOpenAIRequest
				if !assert.NoError(t, common.Unmarshal(payload, &request)) {
					http.Error(w, "invalid request", http.StatusBadRequest)
					return
				}
				toolResult, image := false, false
				for _, message := range request.Messages {
					if message.Role == "tool" {
						toolResult = message.ToolCallId == "call_a"
						assert.Contains(t, message.StringContent(), "screenshot")
					}
					if message.Role == "user" {
						for _, part := range message.ParseContent() {
							media := part.GetImageMedia()
							image = image || part.Type == "image_url" && media != nil && media.Url == "https://images.example.test/image.png"
						}
					}
				}
				assert.True(t, toolResult)
				assert.True(t, image)
				r.Body = io.NopCloser(bytes.NewReader(payload))
				gatewaySDKUpstream(w, r)
			})
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			token, err := model.GetTokenByKey(key, true)
			require.NoError(t, err)
			token.ConversionPolicy = policy
			require.NoError(t, token.Update())
			body := `{"model":"gpt-4o-mini","max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_a","name":"screen","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_a","content":[{"type":"text","text":"screenshot"},{"type":"image","source":{"type":"url","url":"https://images.example.test/image.png"}}]}]}]}`
			status, response := gatewayStateHTTP(t, base, key, "/v1/messages", body)
			require.Equal(t, http.StatusOK, status, response)
			awaitGatewayPermitsReleased(t)
			assert.EqualValues(t, 1, sends.Load())
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery).Error)
			assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.EqualValues(t, 500, user.UsedQuota)
			assert.Zero(t, user.ReservedQuota)
		})
	}
}
