package router

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayAdvancedCustomRoutePassthroughMatchesPreviewAndWire(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				wire, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				assert.Equal(t, "gpt-4o-mini", gjson.GetBytes(wire, "model").String())
				assert.Equal(t, "0", gjson.GetBytes(wire, "vendor_extension.n").Raw)
				assert.Equal(t, "false", gjson.GetBytes(wire, "vendor_extension.enabled").Raw)
				_, _ = w.Write([]byte(`{"id":"chat_route","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"route answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}}`))
			})
			settings := dto.ChannelOtherSettings{AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{
				{IncomingPath: "/v1/chat/completions", UpstreamPath: "/v1/chat/completions", Converter: relayconvert.ConverterNone, Models: []string{"gpt-4o-mini"}, PassThroughBodyEnabled: enabled},
				{IncomingPath: "/v1/responses", UpstreamPath: "/v1/responses", Converter: relayconvert.ConverterNone},
			}}}
			require.NoError(t, settings.AdvancedCustom.Validate())
			encoded, err := common.Marshal(settings)
			require.NoError(t, err)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Updates(map[string]any{"type": constant.ChannelTypeAdvancedCustom, "settings": string(encoded)}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			token, err := model.GetTokenByKey(key, true)
			require.NoError(t, err)
			token.ConversionPolicy = "strict"
			require.NoError(t, token.Update())
			body := `{"model":"gpt-4o-mini","max_tokens":8,"messages":[{"role":"user","content":"hello"}],"vendor_extension":{"n":0,"enabled":false}}`
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.UnmarshalJsonStr(body, &request))
			requirements, err := relayconvert.AnalyzeRequestJSON(&request, []byte(body))
			require.NoError(t, err)
			channel := &model.Channel{Type: constant.ChannelTypeAdvancedCustom, OtherSettings: string(encoded)}
			_, previewErr := service.PlanChannelConversion(requirements, channel, "/v1/chat/completions", "gpt-4o-mini", types.ConversionLossPolicyStrict)
			status, response := gatewayStateHTTP(t, base, key, "/v1/chat/completions", body)
			if enabled {
				require.NoError(t, previewErr)
				require.Equal(t, http.StatusOK, status, response)
				assert.EqualValues(t, 1, sends.Load())
			} else {
				require.Error(t, previewErr)
				assert.Equal(t, http.StatusBadRequest, status, response)
				assert.Zero(t, sends.Load())
			}
			awaitGatewayPermitsReleased(t)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.Zero(t, user.ReservedQuota)
			if enabled {
				assert.EqualValues(t, 500, user.UsedQuota)
			} else {
				assert.Zero(t, user.UsedQuota)
			}
			assert.False(t, settings.AdvancedCustom.BodyPassThroughEnabled("/v1/responses", "gpt-4o-mini"))
			assert.False(t, settings.AdvancedCustom.BodyPassThroughEnabled("/v1/chat/completions", "other-model"))
			settings.AdvancedCustom.Routes[0].Converter = relayconvert.ConverterOpenAIChatToClaudeMessages
			settings.AdvancedCustom.Routes[0].PassThroughBodyEnabled = true
			require.Error(t, settings.AdvancedCustom.Validate())
		})
	}
}
