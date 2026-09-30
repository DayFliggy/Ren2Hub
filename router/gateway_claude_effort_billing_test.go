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
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayClaudeEffortProtocolAndIncomingParamBilling(t *testing.T) {
	const beta = "mid-conversation-output-config-2026-07-01"
	const user = `{"role":"user","content":"continue"}`
	const high = `{"role":"system","content":[],"output_config":{"effort":"high"}}`
	const low = `{"role":"system","content":[],"output_config":{"effort":"low"}}`
	const invalid = `{"role":"system","content":[],"output_config":{"effort":42}}`
	for _, test := range []struct {
		name, messages, override, effort, invalidPath string
		quota                                         common.Quota
		retry                                         bool
	}{
		{name: "message overrides top level", messages: high + "," + user, effort: "high", quota: 5000},
		{name: "pending switch", messages: user + "," + high, effort: "medium", quota: 5000},
		{name: "pending switch preserves active effort", messages: high + "," + user + "," + low, effort: "high", quota: 5000},
		{name: "override adds message effort", messages: user, override: `{"messages":[` + high + "," + user + `]}`, effort: "high", quota: 5000},
		{name: "override changes message effort", messages: high + "," + user, override: `{"messages":[` + low + "," + user + `]}`, effort: "low", quota: 5000},
		{name: "override removes message effort", messages: high + "," + user, override: `{"messages":[` + user + `]}`, effort: "medium", quota: 5000},
		{name: "override removes all effort", messages: high + "," + user, override: `{"output_config":null,"messages":[` + user + `]}`, quota: 5000},
		{name: "override changes activation order", messages: high + "," + user, override: `{"messages":[` + user + "," + high + `]}`, effort: "medium", quota: 5000},
		{name: "retry rebuilds source messages", messages: high + "," + user, override: `{"messages":[` + low + "," + user + `]}`, effort: "high", quota: 5000, retry: true},
		{name: "invalid message effort", messages: invalid + "," + user, invalidPath: "messages[0].output_config.effort"},
		{name: "invalid overridden message effort", messages: high + "," + user, override: `{"messages":[` + invalid + "," + user + `]}`, invalidPath: "messages[0].output_config.effort"},
		{name: "invalid pending effort", messages: user + "," + invalid, invalidPath: "messages[1].output_config.effort"},
	} {
		for _, passthrough := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/passthrough=%t", test.name, passthrough), func(t *testing.T) {
				var sends atomic.Int64
				observed := make(chan []byte, 2)
				base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, request *http.Request) {
					attempt := sends.Add(1)
					payload, err := io.ReadAll(request.Body)
					if !assert.NoError(t, err) {
						http.Error(w, "cannot inspect request", http.StatusBadRequest)
						return
					}
					assert.Equal(t, "/v1/messages", request.URL.Path)
					assert.Equal(t, beta, request.Header.Get("anthropic-beta"))
					observed <- payload
					w.Header().Set("Content-Type", "application/json")
					if test.retry && attempt == 1 {
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = io.WriteString(w, `{"error":{"type":"overloaded_error","message":"temporary fixture failure"}}`)
						return
					}
					_, _ = io.WriteString(w, `{"id":"msg_effort","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":1000,"output_tokens":0}}`)
				})
				settings, err := common.Marshal(map[string]any{"pass_through_body_enabled": passthrough})
				require.NoError(t, err)
				var channel model.Channel
				require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeAnthropic).First(&channel).Error)
				channel.Models = "claude-opus-5-5"
				require.NoError(t, model.DB.Model(&channel).Updates(map[string]any{"models": channel.Models, "setting": string(settings), "param_override": test.override}).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
				if test.retry {
					sqlDB, err := model.DB.DB()
					require.NoError(t, err)
					sqlDB.SetMaxOpenConns(1)
					fallback := channel
					fallback.Id, fallback.Key, fallback.Name = 0, "local-effort-fallback", "effort fallback"
					priority, setting := int64(-1), string(settings)
					fallback.Priority, fallback.Setting, fallback.ParamOverride = &priority, &setting, nil
					require.NoError(t, model.DB.Create(&fallback).Error)
					require.NoError(t, fallback.AddAbilities(model.DB))
				}
				_, err = setGatewayExpression(channel.Models, `v1:p * (param("output_config.effort") == "medium" ? 10 : 1)`)
				require.NoError(t, err)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				var before, after model.User
				require.NoError(t, model.DB.Where("username = ?", "sdk-isolated").First(&before).Error)
				body := `{"model":"claude-opus-5-5","max_tokens":64,"output_config":{"effort":"medium"},"messages":[` + test.messages + `]}`
				request, err := http.NewRequest(http.MethodPost, base+"/v1/messages", strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+key)
				request.Header.Set("anthropic-beta", beta)
				response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				output, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				awaitGatewayPermitsReleased(t)
				require.NoError(t, model.DB.First(&after, before.Id).Error)
				if test.invalidPath != "" {
					assert.Equal(t, http.StatusBadRequest, response.StatusCode, string(output))
					assert.Contains(t, string(output), test.invalidPath)
					assert.Zero(t, sends.Load())
					assert.Equal(t, before.Quota, after.Quota)
					assert.Equal(t, before.UsedQuota, after.UsedQuota)
					assert.Zero(t, after.ReservedQuota)
					var events, recoveries int64
					require.NoError(t, model.DB.Model(&model.BillingEvent{}).Count(&events).Error)
					require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
					assert.Zero(t, events, "invalid final request cannot reserve funds")
					assert.Zero(t, recoveries)
					return
				}
				require.Equal(t, http.StatusOK, response.StatusCode, string(output))
				if test.retry {
					require.EqualValues(t, 2, sends.Load())
					firstWire := <-observed
					assert.JSONEq(t, gjson.Get(test.override, "messages").Raw, gjson.GetBytes(firstWire, "messages").Raw)
					assert.Equal(t, "medium", gjson.GetBytes(firstWire, "output_config.effort").String())
				} else {
					require.EqualValues(t, 1, sends.Load())
				}
				wire := <-observed
				expectedMessages := "[" + test.messages + "]"
				if test.override != "" && !test.retry {
					expectedMessages = gjson.Get(test.override, "messages").Raw
				}
				assert.JSONEq(t, expectedMessages, gjson.GetBytes(wire, "messages").Raw)
				if test.effort != "" {
					assert.Equal(t, "medium", gjson.GetBytes(wire, "output_config.effort").String(), "effective effort must not rewrite the top-level configuration")
				}
				var recovery model.BillingRecovery
				require.NoError(t, model.DB.Where("user_id = ?", before.Id).First(&recovery).Error)
				require.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
				var quote billing.Quote
				require.NoError(t, common.UnmarshalJsonStr(recovery.QuoteSnapshot, &quote))
				if test.effort == "" {
					assert.NotContains(t, quote.Attributes, "effort")
				} else {
					assert.Equal(t, test.effort, quote.Attributes["effort"])
				}
				assert.Equal(t, test.quota, before.Quota-after.Quota)
				assert.Equal(t, test.quota, after.UsedQuota-before.UsedQuota)
				assert.Zero(t, after.ReservedQuota)
				var charge billing.ChargeResult
				require.NoError(t, common.UnmarshalJsonStr(recovery.ChargeSnapshot, &charge))
				assert.Equal(t, test.quota, charge.FinalQuota)
				if test.quota == 5000 {
					assert.Equal(t, "0.01", charge.TotalUSD)
				}
				var log model.Log
				require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).First(&log).Error)
				assert.Equal(t, test.effort, gjson.Get(log.Other, "reasoning_effort").String())
				if test.effort == "" {
					assert.False(t, gjson.Get(log.Other, "reasoning_effort").Exists())
				}
				if test.name == "message overrides top level" && !passthrough {
					_, err = setGatewayExpression(channel.Models, `v1:p * 100`)
					require.NoError(t, err)
					var usage billing.Usage
					require.NoError(t, common.UnmarshalJsonStr(string(recovery.UsageSnapshot), &usage))
					var attempt model.BillingAttempt
					require.NoError(t, model.DB.Where("request_id = ? AND quote_hash = ?", recovery.RequestID, quote.Hash).First(&attempt).Error)
					replayed, err := model.ReplayBilling(&quote, usage, recovery.RequestID, attempt.ID, "settlement")
					require.NoError(t, err)
					assert.Equal(t, charge, *replayed, "frozen quote must ignore later price publication")
					_, err = service.RetryBillingRecovery(context.Background(), recovery.RequestID)
					require.NoError(t, err)
					var afterReplay model.User
					require.NoError(t, model.DB.First(&afterReplay, before.Id).Error)
					assert.Equal(t, after.Quota, afterReplay.Quota)
					assert.Equal(t, after.UsedQuota, afterReplay.UsedQuota)
					var replayRecovery model.BillingRecovery
					require.NoError(t, model.DB.First(&replayRecovery, recovery.ID).Error)
					assert.Equal(t, recovery.QuoteSnapshot, replayRecovery.QuoteSnapshot)
					assert.Equal(t, recovery.ChargeSnapshot, replayRecovery.ChargeSnapshot)
					assert.EqualValues(t, 1, sends.Load())
				}
			})
		}
	}
}
