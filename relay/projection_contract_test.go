package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextProjectionPolicyMatchesPlanningAndExecution(t *testing.T) {
	for _, adapter := range []struct {
		name, model  string
		channel, api int
	}{
		{"nova", "nova-lite", constant.ChannelTypeAws, constant.APITypeAws},
		{"coze", "coze-test", constant.ChannelTypeCoze, constant.APITypeCoze},
	} {
		for _, sample := range []struct {
			name, body, path string
			warning          bool
		}{
			{"tools", `{"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`, "tools", false},
			{"serial_false", `{"parallel_tool_calls":false}`, "parallel_tool_calls", false},
			{"tool_history", `{"messages":[{"role":"user","content":"first"},{"role":"assistant","content":"hi","tool_calls":[{"id":"call1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call1","content":"result"},{"role":"user","content":"next"}]}`, "messages[1].tool_calls", false},
			{"image", `{"messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"https://must-not-fetch.invalid/image"}}]}]}`, "messages[0].content[1]", false},
			{"schema", `{"response_format":{"type":"json_schema","json_schema":{"name":"output","strict":true,"schema":{"type":"object"}}}}`, "response_format", false},
			{"developer", `{"messages":[{"role":"developer","content":"rule"},{"role":"user","content":"hi"}]}`, "messages[0].role", false},
			{"zero_warning", `{"frequency_penalty":0}`, "frequency_penalty", true},
		} {
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				t.Run(adapter.name+"/"+sample.name+"/"+string(policy), func(t *testing.T) {
					request := &dto.GeneralOpenAIRequest{Model: adapter.model, Messages: []dto.Message{{Role: "user", Content: "hi"}}}
					require.NoError(t, common.Unmarshal([]byte(sample.body), request))
					info := &relaycommon.RelayInfo{Request: request, RelayFormat: types.RelayFormatOpenAI, OriginModelName: adapter.model, ConversionPolicy: policy}
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
					planErr := PlanRequestForChannel(c, info, &model.Channel{Id: 1, Type: adapter.channel})
					_, convertErr := GetAdaptor(adapter.api).ConvertOpenAIRequest(c, info, request)
					allowed := policy == types.ConversionLossPolicyAllow || (sample.warning && policy == types.ConversionLossPolicySafe)
					if allowed {
						require.NoError(t, planErr)
						require.NoError(t, convertErr)
						require.NotNil(t, info.ConversionPlan)
						assert.False(t, info.ConversionPlan.Native)
						assert.NotEmpty(t, info.ConversionPlan.RequestProjection)
					} else {
						require.Error(t, planErr)
						require.Error(t, convertErr)
					}
					var paths []string
					for _, diagnostic := range info.ConversionDiagnostics {
						paths = append(paths, diagnostic.Path)
					}
					assert.Contains(t, paths, sample.path)
					assert.False(t, info.UpstreamSent)
					assert.False(t, c.Writer.Written())
				})
			}
		}
	}
}

func TestCozeOutputLimitMustNotBeSilentlyIgnored(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "coze", MaxTokens: new(uint)}
	info := &relaycommon.RelayInfo{Request: request, RelayFormat: types.RelayFormatOpenAI}
	require.ErrorContains(t, PlanRequestForChannel(nil, info, &model.Channel{Type: constant.ChannelTypeCoze}), "max_tokens")
	request.MaxTokens = nil
	require.NoError(t, PlanRequestForChannel(nil, info, &model.Channel{Type: constant.ChannelTypeCoze}))
}

func TestProjectionExecutionKeepsSelectedPolicyAfterRequestChanges(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "nova-lite", Messages: []dto.Message{{Role: "user", Content: "hi"}}}
	info := &relaycommon.RelayInfo{Request: request, RelayFormat: types.RelayFormatOpenAI, ConversionPolicy: types.ConversionLossPolicyStrict}
	require.NoError(t, PlanRequestForChannel(nil, info, &model.Channel{Type: constant.ChannelTypeAws}))
	request.FrequencyPenalty = new(float64)
	info.ConversionPolicy = types.ConversionLossPolicyAllow
	_, err := GetAdaptor(constant.APITypeAws).ConvertOpenAIRequest(nil, info, request)
	require.ErrorContains(t, err, "frequency_penalty")
	assert.False(t, info.UpstreamSent)
}
