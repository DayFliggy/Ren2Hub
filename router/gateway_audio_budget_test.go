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
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewaySpeechFinalInputAccounting(t *testing.T) {
	for _, provider := range []struct {
		name        string
		channelType int
		metadata    string
		response    string
	}{
		{"minimax", constant.ChannelTypeMiniMax, `{"text":%q}`, `{"base_resp":{"status_code":0},"data":{"audio":"00010203","status":2}}`},
		{"volc", constant.ChannelTypeVolcEngine, `{"request":{"text":%q}}`, `{"code":3000,"data":"AAECAw=="}`},
	} {
		for _, insufficient := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/insufficient=%t", provider.name, insufficient), func(t *testing.T) {
				finalText := strings.Repeat("x", 400)
				var sends atomic.Int32
				url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					var payload map[string]any
					require.NoError(t, common.DecodeJson(r.Body, &payload))
					if provider.name == "volc" {
						payload = payload["request"].(map[string]any)
					}
					assert.Equal(t, finalText, payload["text"])
					var user model.User
					require.NoError(t, model.DB.First(&user, 1).Error)
					assert.Equal(t, 10000000-400, user.Quota, "reserve the actual provider input before sending")
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, provider.response)
				})
				oldRatios, oldPreconsume, oldCount := ratio_setting.ModelRatio2JSONString(), common.PreConsumedQuota, constant.CountToken
				t.Cleanup(func() {
					require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios))
					common.PreConsumedQuota, constant.CountToken = oldPreconsume, oldCount
				})
				common.PreConsumedQuota, constant.CountToken = 1, false
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"tts-budget":1}`))
				channel, err := model.GetChannelById(1, true)
				require.NoError(t, err)
				channel.Type, channel.Models = provider.channelType, "tts-budget"
				channel.Key = "local-key"
				if provider.name == "volc" {
					channel.Key = "local-app|local-token"
				}
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "key": channel.Key}).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
				quota := 2000
				if insufficient {
					quota = 100
				}
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "remain_quota": quota}).Error)
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
				body := fmt.Sprintf(`{"model":"tts-budget","input":"x","voice":"alloy","response_format":"pcm","metadata":%s}`, fmt.Sprintf(provider.metadata, finalText))
				code, output := postGatewayTask(t, url, key, "/v1/audio/speech", body)
				if insufficient {
					assert.Equal(t, http.StatusForbidden, code, output)
					assert.Zero(t, sends.Load())
				} else {
					assert.Equal(t, http.StatusOK, code, output)
					assert.Equal(t, "\x00\x01\x02\x03", output)
					assert.EqualValues(t, 1, sends.Load())
				}
				awaitRealtimeLeasesReleased(t)
				var user model.User
				var token model.Token
				require.NoError(t, model.DB.First(&user, 1).Error)
				require.NoError(t, model.DB.First(&token, 1).Error)
				wantCharge := 400
				if insufficient {
					wantCharge = 0
				}
				assert.Equal(t, 10000000-wantCharge, user.Quota)
				assert.Equal(t, wantCharge, user.UsedQuota)
				assert.Equal(t, quota-wantCharge, token.RemainQuota)
				var logs []model.Log
				require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
				if insufficient {
					assert.Empty(t, logs)
				} else {
					require.Len(t, logs, 1)
					assert.Equal(t, 400, logs[0].PromptTokens)
				}
			})
		}
	}
}
