package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayResponsesNamespaceHistoryPolicyBeforeBilling(t *testing.T) {
	for _, target := range []struct {
		name        string
		channelType int
		format      types.RelayFormat
	}{
		{"chat", constant.ChannelTypeAdvancedCustom, types.RelayFormatOpenAI},
		{"claude", constant.ChannelTypeAnthropic, types.RelayFormatClaude},
		{"gemini", constant.ChannelTypeGemini, types.RelayFormatGemini},
		{"native", constant.ChannelTypeOpenAI, types.RelayFormatOpenAIResponses},
	} {
		for _, policy := range []string{"safe", "strict", "allow"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", target.name, policy, stream), func(t *testing.T) {
					var sends atomic.Int64
					gatewayURL, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
						sends.Add(1)
						payload, err := io.ReadAll(r.Body)
						if !assert.NoError(t, err) {
							http.Error(w, "cannot inspect request", http.StatusBadRequest)
							return
						}
						if target.name == "native" {
							assert.Equal(t, "docs", gjson.GetBytes(payload, "input.0.namespace").String())
							assert.Equal(t, "files", gjson.GetBytes(payload, "input.1.namespace").String())
							assert.Equal(t, "read", gjson.GetBytes(payload, "input.0.name").String())
							assert.Equal(t, "read", gjson.GetBytes(payload, "input.1.name").String())
						}
						r.Body = io.NopCloser(bytes.NewReader(payload))
						gatewaySDKUpstream(w, r)
					})
					channel, err := model.GetChannelById(1, true)
					require.NoError(t, err)
					channel.Type = target.channelType
					if target.name == "chat" {
						channel.SetOtherSettings(dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
							IncomingPath: "/v1/responses", UpstreamPath: "/v1/chat/completions", Converter: "openai_responses_to_openai_chat_completions",
						}}}})
					}
					require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "settings": channel.OtherSettings}).Error)
					require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
					require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Updates(map[string]any{
						"conversion_policy": policy, "unlimited_quota": false, "available_quota": 2000,
					}).Error)
					var token model.Token
					require.NoError(t, model.DB.Where("key = ?", key).First(&token).Error)
					profile, err := service.CreateUserRouteProfile(service.RouteProfileInput{
						UserID: token.UserId, TokenID: token.Id, Mode: model.RouteModeManual,
						Groups: []service.RouteGroupInput{{Name: "namespace", Enabled: true,
							Entries: []service.RouteEntryInput{{ChannelID: channel.Id, Source: model.RouteSourcePlatform, Enabled: true}},
						}},
					})
					require.NoError(t, err)
					body := fmt.Sprintf(`{"model":"gpt-4o-mini","max_output_tokens":64,"stream":%t,"input":[{"type":"function_call","call_id":"call_docs","namespace":"docs","name":"read","arguments":"{}"},{"type":"function_call","call_id":"call_files","namespace":"files","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_docs","output":"docs result"},{"type":"function_call_output","call_id":"call_files","output":"files result"}]}`, stream)
					preview, err := service.PreviewUserRouteProfile(context.Background(), token.UserId, profile.Profile.ID,
						service.RouteProfilePreviewInput{Model: "gpt-4o-mini", Path: "/v1/responses", Request: json.RawMessage(body)})
					require.NoError(t, err)
					require.Len(t, preview.Entries, 1)
					entry := preview.Entries[0]
					require.NotNil(t, entry.ConversionPlan)
					assert.Equal(t, target.format, entry.ConversionPlan.To)
					assert.False(t, preview.LiveSelection)
					assert.Zero(t, sends.Load(), "preview must not contact the provider")
					var recoveries int64
					require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
					assert.Zero(t, recoveries, "preview must not precharge")
					rejected := target.name != "native" && policy != "allow"
					if target.name != "native" {
						assert.Contains(t, entry.Diagnostics, types.ConversionDiagnostic{
							Code: "unsupported_tool_semantics", Path: "input[0].namespace",
							Message:  "cannot preserve tool namespace and its configuration",
							Severity: types.ConversionDiagnosticError,
							From:     types.RelayFormatOpenAIResponses, To: target.format,
						})
					}
					if rejected {
						assert.Empty(t, preview.CandidateChannelIDs)
						assert.Equal(t, service.RoutePreviewFilterConversion, entry.FilterReason)
					} else {
						assert.Equal(t, []int{channel.Id}, preview.CandidateChannelIDs)
						assert.Empty(t, entry.FilterReason)
					}
					status, output := gatewayStateHTTP(t, gatewayURL, key, "/v1/responses", body)
					if !rejected {
						require.Equal(t, http.StatusOK, status, output)
						assert.EqualValues(t, 1, sends.Load())
						return
					}
					assert.Equal(t, http.StatusBadRequest, status, output)
					assert.Contains(t, output, "input[0].namespace")
					assert.Zero(t, sends.Load())
					require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
					assert.Zero(t, recoveries)
					var user model.User
					require.NoError(t, model.DB.First(&user, token.UserId).Error)
					require.NoError(t, model.DB.First(&token, token.Id).Error)
					assert.EqualValues(t, 10000000, user.Quota)
					assert.Zero(t, user.UsedQuota)
					assert.EqualValues(t, 2000, token.RemainQuota)
					assert.Zero(t, token.UsedQuota)
					var consumeLogs int64
					require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogs).Error)
					assert.Zero(t, consumeLogs)
					var failures int64
					require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
					assert.Zero(t, failures)
					assert.Equal(t, common.TokenStatusEnabled, token.Status)
				})
			}
		}
	}
}
