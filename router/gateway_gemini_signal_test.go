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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/require"
)

func enableGeminiSignalSuccessLimit(t *testing.T) {
	t.Helper()
	previousEnabled, previousCount, previousSuccess, previousDuration := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes = true, 100, 1, 1
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousCount, previousSuccess, previousDuration
	})
}

func useGeminiSignalChannel(t *testing.T, channelType int) int {
	t.Helper()
	// Exercise an actual reservation and refund instead of the trusted-wallet bypass.
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 100000}).Error)
	channel, err := model.GetChannelById(3, true)
	require.NoError(t, err)
	if channelType == constant.ChannelTypeVertexAi {
		channel.Type = channelType
		channel.OtherSettings = `{"vertex_key_type":"api_key"}`
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "settings": channel.OtherSettings}).Error)
		require.NoError(t, channel.UpdateAbilities(model.DB))
		require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	}
	return channel.Id
}

func geminiSignalRequest(protocol string, stream bool) (string, string) {
	switch protocol {
	case "chat":
		return "/v1/chat/completions", fmt.Sprintf(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hello"}],"max_tokens":10,"stream":%t}`, stream)
	case "responses":
		return "/v1/responses", fmt.Sprintf(`{"model":"gemini-2.5-flash","input":"hello","max_output_tokens":10,"stream":%t}`, stream)
	case "anthropic":
		return "/v1/messages", fmt.Sprintf(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hello"}],"max_tokens":10,"stream":%t}`, stream)
	default:
		action := "generateContent"
		if stream {
			action = "streamGenerateContent?alt=sse"
		}
		return "/v1beta/models/gemini-2.5-flash:" + action, `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":10}}`
	}
}

func TestGatewayGeminiSignalFailures(t *testing.T) {
	enableGeminiSignalSuccessLimit(t)
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
			for _, stream := range []bool{false, true} {
				for _, tc := range []struct {
					name, body string
					status     int
					penalize   bool
				}{
					{"quota", `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, 429, true},
					{"empty", `{}`, 502, true},
					{"prompt_policy", `{"promptFeedback":{"blockReason":"SAFETY"}}`, 400, false},
					{"candidate_policy", `{"candidates":[{"finishReason":"SAFETY"}]}`, 400, false},
				} {
					t.Run(fmt.Sprintf("channel=%d/%s/stream=%t/%s", channelType, protocol, stream, tc.name), func(t *testing.T) {
						var sends atomic.Int64
						base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
							sends.Add(1)
							require.Contains(t, strings.ToLower(r.URL.Path), "generatecontent", "upstream must use the Gemini generation API")
							w.Header().Set("Content-Type", "application/json")
							if stream {
								w.Header().Set("Content-Type", "text/event-stream")
								_, _ = fmt.Fprintf(w, "data: %s\n\n", tc.body)
							} else {
								_, _ = fmt.Fprint(w, tc.body)
							}
						})
						channelID := useGeminiSignalChannel(t, channelType)
						path, body := geminiSignalRequest(protocol, stream)
						request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
						require.NoError(t, err)
						request.Header.Set("Authorization", "Bearer "+key)
						request.Header.Set("Content-Type", "application/json")
						response, err := http.DefaultClient.Do(request)
						require.NoError(t, err)
						defer response.Body.Close()
						data, err := io.ReadAll(response.Body)
						require.NoError(t, err)
						require.Equal(t, tc.status, response.StatusCode, "%s", data)
						require.Positive(t, sends.Load())
						require.Contains(t, string(data), `"error"`)
						var recovery model.BillingRecovery
						require.Eventually(t, func() bool {
							return model.DB.First(&recovery).Error == nil && ((tc.name == "empty" && model.BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState)) || (tc.name != "empty" && recovery.Status == model.BillingRecoveryStatusRefunded))
						}, 2*time.Second, 10*time.Millisecond)
						var user model.User
						require.NoError(t, model.DB.First(&user).Error)
						require.Zero(t, user.UsedQuota)
						held := common.Quota(0)
						if tc.name == "empty" {
							held = recovery.FundingAmount
							require.Positive(t, held)
						}
						require.EqualValues(t, 10000000-held, user.Quota)
						require.Equal(t, held, user.ReservedQuota)
						var token model.Token
						require.NoError(t, model.DB.First(&token).Error)
						require.EqualValues(t, 100000-held, token.RemainQuota)
						require.Equal(t, held, token.ReservedQuota)
						require.Zero(t, token.UsedQuota)
						var health []model.ChannelHealth
						require.NoError(t, model.DB.Where("channel_id = ?", channelID).Find(&health).Error)
						failureCount := 0
						for _, item := range health {
							failureCount += item.FailureCount
						}
						require.Equal(t, tc.penalize, failureCount > 0)
						ctx := context.Background()
						rateKey := service.GatewayLogicalRateKey(user.Id)
						require.Zero(t, common.RDB.ZCard(ctx, rateKey+":pending").Val())
						require.Zero(t, common.RDB.ZCard(ctx, rateKey+":success").Val())
						require.Equal(t, int64(1), common.RDB.ZCard(ctx, rateKey+":total").Val())
					})
				}
			}
		}
	}
}

func TestGatewayGeminiPartialAndNormalTermination(t *testing.T) {
	enableGeminiSignalSuccessLimit(t)
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
			for _, reason := range []string{"error", "SAFETY", "MAX_TOKENS"} {
				t.Run(fmt.Sprintf("channel=%d/%s/%s", channelType, protocol, reason), func(t *testing.T) {
					var sends atomic.Int64
					base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
						sends.Add(1)
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: "+`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}`+"\n\n")
						if reason == "error" {
							_, _ = fmt.Fprint(w, "data: "+`{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`+"\n\n")
						} else {
							_, _ = fmt.Fprintf(w, "data: {\"candidates\":[{\"index\":0,\"finishReason\":%q}]}\n\n", reason)
						}
					})
					channelID := useGeminiSignalChannel(t, channelType)
					path, body := geminiSignalRequest(protocol, true)
					request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
					require.NoError(t, err)
					request.Header.Set("Authorization", "Bearer "+key)
					request.Header.Set("Content-Type", "application/json")
					response, err := http.DefaultClient.Do(request)
					require.NoError(t, err)
					defer response.Body.Close()
					data, readErr := io.ReadAll(response.Body)
					if protocol == "gemini" && reason == "error" {
						require.Error(t, readErr, "native Gemini interruption must also abort transport for SDK accumulators")
					} else {
						require.NoError(t, readErr)
					}
					require.Equal(t, http.StatusOK, response.StatusCode, "%s", data)
					require.Contains(t, string(data), "partial")
					require.Equal(t, int64(1), sends.Load(), "committed output must prevent retries")
					if reason == "error" {
						require.NotContains(t, string(data), "response.completed")
						require.NotContains(t, string(data), "[DONE]")
					} else if reason == "MAX_TOKENS" {
						terminator := map[string]string{
							"chat": `"finish_reason":"length"`, "responses": `"reason":"max_output_tokens"`,
							"anthropic": `"stop_reason":"max_tokens"`, "gemini": `"finishReason":"MAX_TOKENS"`,
						}
						require.Contains(t, string(data), terminator[protocol])
					} else if protocol == "responses" {
						require.Contains(t, string(data), `"reason":"content_filter"`)
					}
					var recovery model.BillingRecovery
					require.Eventually(t, func() bool {
						if model.DB.First(&recovery).Error != nil {
							return false
						}
						if reason == "error" {
							return recovery.Status == model.BillingRecoveryStatusReconciliation
						}
						return recovery.Status == model.BillingRecoveryStatusSettled && recovery.ConsumptionState == model.BillingRecoveryStateCompleted
					}, 2*time.Second, 10*time.Millisecond)
					var user model.User
					require.NoError(t, model.DB.First(&user).Error)
					if reason == "error" {
						require.Zero(t, user.UsedQuota, "partial output cannot confirm final consumption")
						require.Equal(t, recovery.FundingAmount, user.ReservedQuota)
						require.Positive(t, user.ReservedQuota)
					} else {
						require.EqualValues(t, 500, user.UsedQuota, "confirmed terminal settles exactly one fixed-price call")
						require.Zero(t, user.ReservedQuota)
					}
					var health []model.ChannelHealth
					require.NoError(t, model.DB.Where("channel_id = ?", channelID).Find(&health).Error)
					failures := 0
					for _, item := range health {
						failures += item.FailureCount
					}
					require.Equal(t, reason == "error", failures > 0)
					rateKey := service.GatewayLogicalRateKey(user.Id)
					require.Zero(t, common.RDB.ZCard(context.Background(), rateKey+":pending").Val())
					wantSuccess := int64(1)
					if reason == "error" {
						wantSuccess = 0
					}
					require.Equal(t, wantSuccess, common.RDB.ZCard(context.Background(), rateKey+":success").Val())
				})
			}
		}
	}
}

func TestGatewayGeminiJSONErrorForStreamingRequest(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
			t.Run(fmt.Sprintf("channel=%d/%s", channelType, protocol), func(t *testing.T) {
				base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					_, _ = fmt.Fprint(w, `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`)
				})
				useGeminiSignalChannel(t, channelType)
				path, body := geminiSignalRequest(protocol, true)
				request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+key)
				request.Header.Set("Content-Type", "application/json")
				response, err := http.DefaultClient.Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.Equal(t, http.StatusTooManyRequests, response.StatusCode, "%s", data)
				require.Contains(t, string(data), "quota")
				var recovery model.BillingRecovery
				require.NoError(t, model.DB.First(&recovery).Error)
				require.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
			})
		}
	}
}

func TestGatewayGeminiLocalFailureHasNoProviderOrBillingEffects(t *testing.T) {
	enableGeminiSignalSuccessLimit(t)
	for _, channelType := range []int{constant.ChannelTypeGemini, constant.ChannelTypeVertexAi} {
		for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
			t.Run(fmt.Sprintf("channel=%d/%s", channelType, protocol), func(t *testing.T) {
				var sends atomic.Int64
				base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					w.WriteHeader(http.StatusBadGateway)
				})
				useGeminiSignalChannel(t, channelType)
				path, body := geminiSignalRequest(protocol, false)
				var payload map[string]any
				require.NoError(t, common.Unmarshal([]byte(body), &payload))
				switch protocol {
				case "gemini":
					payload["generationConfig"].(map[string]any)["maxOutputTokens"] = "invalid"
				case "responses":
					payload["max_output_tokens"] = "invalid"
				default:
					payload["max_tokens"] = "invalid"
				}
				encoded, err := common.Marshal(payload)
				require.NoError(t, err)
				request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(string(encoded)))
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+key)
				request.Header.Set("Content-Type", "application/json")
				response, err := http.DefaultClient.Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, err)
				require.Equal(t, http.StatusBadRequest, response.StatusCode)
				require.Zero(t, sends.Load())
				var count int64
				require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Count(&count).Error)
				require.Zero(t, count)
				require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
				require.Zero(t, count)
				require.Zero(t, common.RDB.ZCard(context.Background(), service.GatewayLogicalRateKey(1)+":pending").Val())
				require.Zero(t, common.RDB.ZCard(context.Background(), service.GatewayLogicalRateKey(1)+":success").Val())
			})
		}
	}
}
