package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayMediaPlanRejectsBeforeSendAndPreservesImages(t *testing.T) {
	for _, protocol := range []struct {
		name, path, body, image, unsupported string
		unsupportedChannel                   int
	}{
		{"chat", "/v1/chat/completions", `{"model":"media-plan","messages":[{"role":"user","content":%s}],"max_tokens":32,"stream":%t}`, `[{"type":"image_url","image_url":{"url":"https://images.example/picture.png?size=2"}}]`, `[{"type":"input_audio","input_audio":{"data":"YWJj","format":"wav"}}]`, constant.ChannelTypeAnthropic},
		{"responses", "/v1/responses", `{"model":"media-plan","input":[{"role":"user","content":%s}],"max_output_tokens":32,"stream":%t}`, `[{"type":"input_image","image_url":"https://images.example/picture.png?size=2"}]`, `[{"type":"input_audio","input_audio":{"data":"YWJj","format":"wav"}}]`, constant.ChannelTypeAnthropic},
		{"claude", "/v1/messages", `{"model":"media-plan","messages":[{"role":"user","content":%s}],"max_tokens":32,"stream":%t}`, `[{"type":"image","source":{"type":"url","url":"https://images.example/picture.png?size=2"}}]`, `[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERg=="}}]`, constant.ChannelTypeOpenAI},
		{"gemini", "/v1beta/models/media-plan:generateContent", `{"contents":[{"role":"user","parts":%s}],"generationConfig":{"maxOutputTokens":32}}`, `[{"fileData":{"mimeType":"image/png","fileUri":"https://images.example/picture.png?size=2"}}]`, `[{"inlineData":{"mimeType":"application/pdf","data":"JVBERg=="}}]`, constant.ChannelTypeOpenAI},
	} {
		for _, policy := range []string{"safe", "strict", "allow"} {
			for _, stream := range []bool{false, true} {
				for _, rejected := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/stream=%t/reject=%t", protocol.name, policy, stream, rejected), func(t *testing.T) {
						var sends atomic.Int32
						gatewayURL, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
							sends.Add(1)
							assert.False(t, rejected, "unexecutable media must be rejected before opening an upstream request")
							payload, err := io.ReadAll(r.Body)
							require.NoError(t, err)
							imagePath := "messages.0.content.0.image_url.url"
							if protocol.name == "responses" {
								imagePath = "input.0.content.0.image_url"
							}
							assert.Equal(t, "https://images.example/picture.png?size=2", gjson.GetBytes(payload, imagePath).String())
							var user model.User
							require.NoError(t, model.DB.First(&user, 1).Error)
							assert.Equal(t, 9999500, user.Quota, "allowed requests still precharge before sending")
							r.Body = io.NopCloser(bytes.NewReader(payload))
							gatewaySDKUpstream(w, r)
						})
						channel, err := model.GetChannelById(1, true)
						require.NoError(t, err)
						channel.Type, channel.Models = constant.ChannelTypeOpenAI, "media-plan"
						if rejected {
							channel.Type = protocol.unsupportedChannel
						}
						require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
						require.NoError(t, channel.AddAbilities(model.DB))
						require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"media-plan":0.001}`))
						require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"conversion_policy": policy, "unlimited_quota": false, "remain_quota": 2000}).Error)
						require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
						content, path := protocol.image, protocol.path
						if rejected {
							content = protocol.unsupported
						}
						body := ""
						if protocol.name == "gemini" {
							body = fmt.Sprintf(protocol.body, content)
							if stream {
								path = strings.Replace(path, ":generateContent", ":streamGenerateContent?alt=sse", 1)
							}
						} else {
							body = fmt.Sprintf(protocol.body, content, stream)
						}
						status, output := postGatewayTask(t, gatewayURL, key, path, body)
						charge := 500
						if rejected {
							assert.Equal(t, http.StatusBadRequest, status, output)
							assert.Zero(t, sends.Load())
							charge = 0
						} else {
							assert.Equal(t, http.StatusOK, status, output)
							assert.Contains(t, output, "hello")
							assert.EqualValues(t, 1, sends.Load())
						}
						awaitGatewayPermitsReleased(t)
						var user model.User
						var token model.Token
						require.Eventually(t, func() bool {
							return model.DB.First(&user, 1).Error == nil && model.DB.First(&token, 1).Error == nil && user.Quota == 10000000-charge && token.RemainQuota == 2000-charge
						}, time.Second, 10*time.Millisecond)
						assert.Equal(t, charge, user.UsedQuota)
						var logs []model.Log
						require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
						if rejected {
							assert.Empty(t, logs)
						} else {
							assert.Len(t, logs, 1)
						}
						var failures int64
						require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
						assert.Zero(t, failures)
					})
				}
			}
		}
	}
}
