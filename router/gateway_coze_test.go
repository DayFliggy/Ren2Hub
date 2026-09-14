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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayCozeCreationNeverReplaysAfterAcceptance(t *testing.T) {
	for _, scenario := range []string{"malformed_creation", "unknown_acceptance", "poll_not_found", "poll_unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			var creates, polls, fallbacks atomic.Int64
			url, tokenKey, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/v3/chat":
					creates.Add(1)
					if scenario == "malformed_creation" {
						_, _ = io.WriteString(w, `{`)
						return
					}
					if scenario == "unknown_acceptance" {
						connection, _, err := w.(http.Hijacker).Hijack()
						if assert.NoError(t, err) {
							_ = connection.Close()
						}
						return
					}
					_, _ = io.WriteString(w, `{"data":{"id":"accepted-chat","conversation_id":"accepted-conversation"}}`)
				case "/v3/chat/retrieve":
					polls.Add(1)
					status := http.StatusServiceUnavailable
					if scenario == "poll_not_found" {
						status = http.StatusNotFound
					}
					http.Error(w, `{"error":{"message":"status unavailable"}}`, status)
				default:
					fallbacks.Add(1)
					gatewaySDKUpstream(w, request)
				}
			})
			var channel model.Channel
			require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeOpenAI).Order("id").First(&channel).Error)
			require.NoError(t, model.DB.Model(&channel).Updates(map[string]any{"type": constant.ChannelTypeCoze, "other": "fixture-bot"}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			request, err := http.NewRequest(http.MethodPost, url+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}]}`))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+tokenKey)
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			_, err = io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, response.StatusCode, 400)
			assert.EqualValues(t, 1, creates.Load(), "the accepted or ambiguous chat creation must not be repeated")
			assert.Zero(t, fallbacks.Load(), "a query failure must not start a new chat on another channel")
			if strings.HasPrefix(scenario, "poll_") {
				assert.EqualValues(t, 1, polls.Load())
			}
		})
	}
}

func TestGatewayCozeCannotUseStaleTPMAfterChannelTypeChange(t *testing.T) {
	var sends atomic.Int64
	url, tokenKey, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, request *http.Request) {
		sends.Add(1)
		gatewaySDKUpstream(w, request)
	})
	var channel model.Channel
	require.NoError(t, model.DB.Where("type = ?", constant.ChannelTypeOpenAI).Order("id").First(&channel).Error)
	config := model.DefaultGatewayConfig()
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	config.ChannelRates[channel.Id] = model.GatewayProviderRateLimit{TPM: 10000}
	require.NoError(t, model.SaveGatewayConfig(config))
	require.NoError(t, model.DB.Model(&channel).Updates(map[string]any{"type": constant.ChannelTypeCoze, "other": "fixture-bot"}).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", tokenKey).Update("conversion_policy", "allow").Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	request, err := http.NewRequest(http.MethodPost, url+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+tokenKey)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode, string(body))
	assert.Zero(t, sends.Load(), "allow must not claim a bounded budget when the adaptor discards the output limit")
}
