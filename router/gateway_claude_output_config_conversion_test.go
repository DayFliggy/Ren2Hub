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
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayClaudeMessageOutputConfigConversionBeforeBilling(t *testing.T) {
	for _, target := range []struct {
		name        string
		channelType int
		format      types.RelayFormat
		path        string
	}{
		{"chat", constant.ChannelTypeOpenAI, types.RelayFormatOpenAI, "/v1/chat/completions"},
		{"responses", constant.ChannelTypeOpenAI, types.RelayFormatOpenAIResponses, "/v1/responses"},
		{"gemini", constant.ChannelTypeGemini, types.RelayFormatGemini, ""},
		{"native", constant.ChannelTypeAnthropic, types.RelayFormatClaude, "/v1/messages"},
	} {
		for _, policy := range []string{"safe", "strict", "allow"} {
			t.Run(fmt.Sprintf("%s/%s", target.name, policy), func(t *testing.T) {
				var sends atomic.Int64
				gatewayURL, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					payload, err := io.ReadAll(r.Body)
					if !assert.NoError(t, err) {
						http.Error(w, "cannot inspect request", http.StatusBadRequest)
						return
					}
					if target.path != "" {
						assert.Equal(t, target.path, r.URL.Path)
					}
					if target.name == "native" {
						assert.Equal(t, "high", gjson.GetBytes(payload, "messages.2.output_config.effort").String())
						assert.False(t, gjson.GetBytes(payload, "output_config.effort").Exists(), "message effort must not rewrite the top-level configuration")
					} else {
						assert.NotContains(t, string(payload), `"output_config"`)
						assert.NotContains(t, string(payload), `"high"`)
					}
					r.Body = io.NopCloser(bytes.NewReader(payload))
					gatewaySDKUpstream(w, r)
				})
				channel, err := model.GetChannelById(1, true)
				require.NoError(t, err)
				channel.Type = target.channelType
				settings := model_setting.GetGlobalSettings()
				previous := settings.ChatCompletionsToResponsesPolicy
				t.Cleanup(func() { settings.ChatCompletionsToResponsesPolicy = previous })
				settings.ChatCompletionsToResponsesPolicy = model_setting.ChatCompletionsToResponsesPolicy{
					Enabled: target.name == "responses", AllChannels: true, ModelPatterns: []string{"^gpt-4o-mini$"},
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
					Groups: []service.RouteGroupInput{{Name: "message effort", Enabled: true,
						Entries: []service.RouteEntryInput{{ChannelID: channel.Id, Source: model.RouteSourcePlatform, Enabled: true}},
					}},
				})
				require.NoError(t, err)
				body := `{"model":"gpt-4o-mini","max_tokens":64,"messages":[{"role":"user","content":"summary"},{"role":"assistant","content":"done"},{"role":"system","content":[],"output_config":{"effort":"high"}},{"role":"user","content":"continue"}]}`
				preview, err := service.PreviewUserRouteProfile(context.Background(), token.UserId, profile.Profile.ID,
					service.RouteProfilePreviewInput{Model: "gpt-4o-mini", Path: "/v1/messages", Request: json.RawMessage(body)})
				require.NoError(t, err)
				require.Len(t, preview.Entries, 1)
				entry := preview.Entries[0]
				require.NotNil(t, entry.ConversionPlan)
				assert.Equal(t, target.format, entry.ConversionPlan.To)
				assert.False(t, preview.LiveSelection)
				assert.Zero(t, sends.Load())
				var recoveries int64
				require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				assert.Zero(t, recoveries, "preview must not precharge")
				diagnostic := types.ConversionDiagnostic{
					Code: "claude_message_output_config_not_portable", Path: "messages[2].output_config",
					Message:  "conversion cannot preserve Claude per-message output configuration",
					Severity: types.ConversionDiagnosticError, From: types.RelayFormatClaude, To: target.format,
				}
				if target.name != "native" {
					assert.Contains(t, entry.Diagnostics, diagnostic)
				}
				rejected := target.name != "native" && policy != "allow"
				if rejected {
					assert.Empty(t, preview.CandidateChannelIDs)
					assert.Equal(t, service.RoutePreviewFilterConversion, entry.FilterReason)
				} else {
					assert.Equal(t, []int{channel.Id}, preview.CandidateChannelIDs)
					assert.Empty(t, entry.FilterReason)
				}
				status, output := gatewayStateHTTP(t, gatewayURL, key, "/v1/messages", body)
				awaitGatewayPermitsReleased(t)
				if !rejected {
					require.Equal(t, http.StatusOK, status, output)
					assert.EqualValues(t, 1, sends.Load())
					var logs []model.Log
					require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
					require.Len(t, logs, 1)
					var other struct {
						AdminInfo struct {
							Diagnostics []types.ConversionDiagnostic `json:"conversion_diagnostics"`
						} `json:"admin_info"`
					}
					require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
					if target.name != "native" {
						assert.Contains(t, other.AdminInfo.Diagnostics, diagnostic, "allow must record the same loss reported by preview")
					} else {
						assert.NotContains(t, other.AdminInfo.Diagnostics, diagnostic)
					}
					return
				}
				assert.Equal(t, http.StatusBadRequest, status, output)
				assert.Contains(t, output, "messages[2].output_config")
				assert.Zero(t, sends.Load())
				require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				assert.Zero(t, recoveries)
				var user model.User
				require.NoError(t, model.DB.First(&user, token.UserId).Error)
				require.NoError(t, model.DB.First(&token, token.Id).Error)
				assert.EqualValues(t, 10000000, user.Quota)
				assert.Zero(t, user.UsedQuota)
				assert.Zero(t, user.ReservedQuota)
				assert.EqualValues(t, 2000, token.RemainQuota)
				assert.Zero(t, token.UsedQuota)
				assert.Zero(t, token.ReservedQuota)
				var consumeLogs, events int64
				require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumeLogs).Error)
				require.NoError(t, model.DB.Model(&model.BillingEvent{}).Count(&events).Error)
				assert.Zero(t, consumeLogs)
				assert.Zero(t, events, "rejected requests must not reserve or debit funds")
			})
		}
	}
}
