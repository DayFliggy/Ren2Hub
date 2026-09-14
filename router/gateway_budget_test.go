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
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayFinalBudgetRejectsBeforeDebitOrSend(t *testing.T) {
	for _, test := range []struct {
		name, model, path, body, override string
		status                            int
		tpm                               int64
		quota                             int
	}{
		{"chat", "gpt-4o-mini", "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`, `{"max_tokens":2048}`, 429, 500, 0},
		{"responses", "gpt-4o-mini", "/v1/responses", `{"model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`, `{"max_output_tokens":2048}`, 429, 500, 0},
		{"claude", "claude-3-5-sonnet", "/v1/messages", `{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`, `{"max_tokens":2048}`, 429, 500, 0},
		{"gemini", "gemini-2.5-flash", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"maxOutputTokens":10}}`, `{"generationConfig":{"maxOutputTokens":2048}}`, 429, 500, 0},
		{"invalid_override", "gpt-4o-mini", "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`, `{"max_tokens":-1}`, 400, 0, 0},
		{"insufficient_final_quota", "gpt-4o-mini", "/v1/chat/completions", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hello"}],"max_tokens":10}`, `{"max_tokens":20480}`, 403, 0, 1000},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int64
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
			var channels []model.Channel
			require.NoError(t, model.DB.Where("models = ?", test.model).Find(&channels).Error)
			config := model.DefaultGatewayConfig()
			encoded, err := common.Marshal(config)
			require.NoError(t, err)
			require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
			for _, channel := range channels {
				require.NoError(t, model.DB.Model(&channel).Update("param_override", test.override).Error)
				config.ChannelRates[channel.Id] = model.GatewayProviderRateLimit{TPM: test.tpm}
			}
			require.NoError(t, model.SaveGatewayConfig(config))
			if test.quota > 0 {
				oldRatios := ratio_setting.ModelRatio2JSONString()
				t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(oldRatios)) })
				require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"gpt-4o-mini":1}`))
				require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
				require.NoError(t, model.DB.Model(&model.User{}).Where("username = ?", "sdk-isolated").Update("quota", test.quota).Error)
			}
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			var before model.User
			require.NoError(t, model.DB.Where("username = ?", "sdk-isolated").First(&before).Error)
			request, err := http.NewRequest(http.MethodPost, url+test.path, strings.NewReader(test.body))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+key)
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, test.status, response.StatusCode, string(body))
			assert.Zero(t, sends.Load())
			var after model.User
			require.NoError(t, model.DB.First(&after, before.Id).Error)
			assert.Equal(t, before.Quota, after.Quota)
			var recoveries, failures int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			assert.Zero(t, recoveries)
			assert.Zero(t, failures, "local admission must not penalize provider health")
		})
	}
}
