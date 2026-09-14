package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func legacyProjectionFixture(t *testing.T, channelType int, modelName string) (*gin.Context, *relaycommon.RelayInfo, channel.Adaptor) {
	t.Helper()
	apiType, found := common.ChannelType2APIType(channelType)
	require.True(t, found)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ConversionPolicy: types.ConversionLossPolicySafe,
		ChannelMeta: &relaycommon.ChannelMeta{ApiType: apiType, ChannelType: channelType, UpstreamModelName: modelName,
			ApiKey: "test-app|test-secret|test-key", ChannelBaseUrl: "https://provider.invalid"}}
	if channelType == constant.ChannelTypeTencent {
		info.ApiKey = "123|test-secret|test-key"
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelKey, info.ApiKey)
	adaptor := GetAdaptor(apiType)
	require.NotNil(t, adaptor)
	adaptor.Init(info)
	return c, info, adaptor
}

func TestLegacyRequestProjectionPreservesHistoryAndZeroValues(t *testing.T) {
	for _, tc := range []struct {
		name        string
		channelType int
		model       string
		request     string
		want        string
	}{
		{"cohere conversation", constant.ChannelTypeCohere, "command", `{"model":"command","stream":false,"messages":[{"role":"system","content":"rules"},{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"second"}],"max_tokens":8,"max_completion_tokens":0}`,
			`{"model":"command","chat_history":[{"role":"SYSTEM","message":"rules"},{"role":"USER","message":"first"},{"role":"CHATBOT","message":"answer"}],"message":"second","stream":false,"max_tokens":0}`},
		{"baidu system and zeros", constant.ChannelTypeBaidu, "ERNIE-Bot", `{"model":"ERNIE-Bot","messages":[{"role":"system","content":"first rules"},{"role":"system","content":"second rules"},{"role":"user","content":"question"}],"temperature":0,"top_p":0,"frequency_penalty":0,"max_tokens":1}`,
			`{"messages":[{"role":"user","content":"question"}],"system":"first rules\n\nsecond rules","temperature":0,"top_p":0,"penalty_score":0,"max_output_tokens":1}`},
		{"zhipu no synthetic message", constant.ChannelTypeZhipu, "chatglm_std", `{"model":"chatglm_std","messages":[{"role":"system","content":"rules"},{"role":"user","content":"question"}],"temperature":0,"top_p":0}`,
			`{"prompt":[{"role":"system","content":"rules"},{"role":"user","content":"question"}],"temperature":0,"top_p":0}`},
		{"xunfei sampling and zero limit", constant.ChannelTypeXunfei, "SparkDesk-v3.5", `{"model":"SparkDesk-v3.5","messages":[{"role":"system","content":"rules"},{"role":"user","content":"question"}],"temperature":0,"top_k":0,"max_tokens":8,"max_completion_tokens":0}`,
			`{"header":{"app_id":"test-app"},"parameter":{"chat":{"domain":"generalv3.5","temperature":0,"top_k":0,"max_tokens":0}},"payload":{"message":{"text":[{"role":"system","content":"rules"},{"role":"user","content":"question"}]}}}`},
		{"palm typed prompt", constant.ChannelTypePaLM, "chat-bison-002", `{"model":"chat-bison-002","messages":[{"role":"user","content":"first"},{"role":"assistant","content":"answer"},{"role":"user","content":"question"}],"temperature":0,"top_p":0,"top_k":0}`,
			`{"prompt":{"messages":[{"author":"user","content":"first"},{"author":"assistant","content":"answer"},{"author":"user","content":"question"}]},"temperature":0,"topP":0,"topK":0}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, info, adaptor := legacyProjectionFixture(t, tc.channelType, tc.model)
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(tc.request), &request))
			converted, err := adaptor.ConvertOpenAIRequest(c, info, &request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			// Safety mode is an independently configured Cohere policy.
			if tc.channelType == constant.ChannelTypeCohere {
				var object map[string]any
				require.NoError(t, common.Unmarshal(body, &object))
				delete(object, "safety_mode")
				body, err = common.Marshal(object)
				require.NoError(t, err)
			}
			assert.JSONEq(t, tc.want, string(body))
			if tc.channelType == constant.ChannelTypePaLM {
				endpoint, err := adaptor.GetRequestURL(info)
				require.NoError(t, err)
				assert.Equal(t, "https://provider.invalid/v1beta2/models/chat-bison-002:generateMessage", endpoint)
				info.UpstreamModelName = "PaLM-2"
				endpoint, err = adaptor.GetRequestURL(info)
				require.NoError(t, err)
				assert.Equal(t, "https://provider.invalid/v1beta2/models/chat-bison-001:generateMessage", endpoint)
			}
		})
	}
}

func TestLegacyRequestProjectionDoesNotInventMissingLimits(t *testing.T) {
	for _, tc := range []struct {
		channelType int
		path        string
	}{
		{constant.ChannelTypeBaidu, "max_output_tokens"},
		{constant.ChannelTypeCohere, "max_tokens"},
		{constant.ChannelTypeXunfei, "parameter.chat.max_tokens"},
	} {
		t.Run(constant.GetChannelTypeName(tc.channelType), func(t *testing.T) {
			c, info, adaptor := legacyProjectionFixture(t, tc.channelType, "test-model")
			request := &dto.GeneralOpenAIRequest{Model: "test-model", Messages: []dto.Message{{Role: "user", Content: "question"}}}
			for _, limit := range []*uint{nil, new(uint)} {
				request.MaxTokens = limit
				if limit != nil {
					request.Stream = new(bool)
				}
				converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
				require.NoError(t, err)
				body, err := common.Marshal(converted)
				require.NoError(t, err)
				value := gjson.GetBytes(body, tc.path)
				assert.Equal(t, limit != nil, value.Exists())
				if tc.channelType != constant.ChannelTypeXunfei {
					stream := gjson.GetBytes(body, "stream")
					assert.Equal(t, limit != nil, stream.Exists())
					assert.False(t, stream.Bool())
				}
				if limit != nil {
					assert.Equal(t, int64(0), value.Int())
				}
			}
		})
	}
	_, info, adaptor := legacyProjectionFixture(t, constant.ChannelTypeZhipu, "chatglm_std")
	request := &dto.GeneralOpenAIRequest{Model: "chatglm_std", TopP: new(float64), Messages: []dto.Message{{Role: "user", Content: "question"}}}
	*request.TopP = 1
	converted, err := adaptor.ConvertOpenAIRequest(nil, info, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.Equal(t, float64(1), gjson.GetBytes(body, "top_p").Float())
	assert.Equal(t, float64(1), *request.TopP, "projection must not mutate caller sampling parameters")
}

func TestLegacyProjectionPlanRejectsUnsupportedSemantics(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeBaidu, constant.ChannelTypeCohere, constant.ChannelTypeZhipu, constant.ChannelTypeTencent, constant.ChannelTypeXunfei, constant.ChannelTypePaLM} {
		t.Run(constant.GetChannelTypeName(channelType), func(t *testing.T) {
			for _, tc := range []struct {
				name string
				body string
			}{
				{"tools", `{"model":"test-model","messages":[{"role":"user","content":"question"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`},
				{"structured output", `{"model":"test-model","messages":[{"role":"user","content":"question"}],"response_format":{"type":"json_object"}}`},
				{"media", `{"model":"test-model","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://private.invalid/image"}}]}]}`},
				{"multiple candidates", `{"model":"test-model","messages":[{"role":"user","content":"question"}],"n":2}`},
			} {
				t.Run(tc.name, func(t *testing.T) {
					c, info, adaptor := legacyProjectionFixture(t, channelType, "test-model")
					var request dto.GeneralOpenAIRequest
					require.NoError(t, common.Unmarshal([]byte(tc.body), &request))
					requirements, err := relayconvert.AnalyzeRequest(&request)
					require.NoError(t, err)
					plan, err := service.PlanChannelConversion(requirements, &model.Channel{Type: channelType}, "/v1/chat/completions", request.Model, types.ConversionLossPolicySafe)
					require.Error(t, err)
					assert.NotEmpty(t, plan.Diagnostics)
					_, err = adaptor.ConvertOpenAIRequest(c, info, &request)
					require.Error(t, err)
					assert.False(t, c.Writer.Written())
				})
			}
		})
	}
}

func TestLegacyProjectionRejectsUnsupportedLimitsAndMessageOrder(t *testing.T) {
	for _, tc := range []struct {
		channelType int
		request     string
	}{
		{constant.ChannelTypeZhipu, `{"model":"test-model","messages":[{"role":"user","content":"question"}],"max_tokens":1}`},
		{constant.ChannelTypeTencent, `{"model":"test-model","messages":[{"role":"user","content":"question"}],"max_tokens":1}`},
		{constant.ChannelTypePaLM, `{"model":"test-model","messages":[{"role":"user","content":"question"}],"max_tokens":1}`},
		{constant.ChannelTypeXunfei, `{"model":"SparkDesk-v2.1","messages":[{"role":"system","content":"rules"},{"role":"user","content":"question"}]}`},
		{constant.ChannelTypeCohere, `{"model":"test-model","messages":[{"role":"user","content":"question"},{"role":"assistant","content":"prefill"}]}`},
		{constant.ChannelTypeBaidu, `{"model":"test-model","messages":[{"role":"user","content":"question"},{"role":"system","content":"later rules"}]}`},
	} {
		t.Run(constant.GetChannelTypeName(tc.channelType), func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(tc.request), &request))
			c, info, adaptor := legacyProjectionFixture(t, tc.channelType, request.Model)
			requirements, err := relayconvert.AnalyzeRequest(&request)
			require.NoError(t, err)
			_, err = service.PlanChannelConversion(requirements, &model.Channel{Type: tc.channelType}, "/v1/chat/completions", request.Model, types.ConversionLossPolicySafe)
			require.Error(t, err)
			_, err = adaptor.ConvertOpenAIRequest(c, info, &request)
			require.Error(t, err)
			if tc.channelType == constant.ChannelTypeCohere || tc.channelType == constant.ChannelTypeBaidu {
				info.ConversionPolicy = types.ConversionLossPolicyAllow
				_, err = service.PlanChannelConversion(requirements, &model.Channel{Type: tc.channelType}, "/v1/chat/completions", request.Model, types.ConversionLossPolicyAllow)
				require.Error(t, err, "allow cannot select a message order with no implemented projection")
				_, err = adaptor.ConvertOpenAIRequest(c, info, &request)
				require.Error(t, err)
			}
		})
	}
}

func TestTencentTokenHubKeepsNativeRequestFeatures(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Model: "hunyuan", MaxTokens: new(uint),
		Messages: []dto.Message{{Role: "user", Content: "question"}}, ResponseFormat: &dto.ResponseFormat{Type: "json_object"}}
	requirements, err := relayconvert.AnalyzeRequest(request)
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		keys string
		ok   bool
	}{
		{"tokenhub", "test-tokenhub-a\ntest-tokenhub-b", true},
		{"mixed", "test-tokenhub-a\n123|test-secret|test-key", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := service.PlanChannelConversion(requirements, &model.Channel{Type: constant.ChannelTypeTencent, Key: tc.keys}, "/v1/chat/completions", request.Model, types.ConversionLossPolicySafe)
			if !tc.ok {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.True(t, plan.Native)
			assert.Empty(t, plan.RequestProjection)
			c, info, _ := legacyProjectionFixture(t, constant.ChannelTypeTencent, request.Model)
			info.ApiKey, info.ConversionPlan = "test-tokenhub-a", &plan
			adaptor := GetAdaptor(constant.APITypeTencent)
			adaptor.Init(info)
			converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.Equal(t, "json_object", gjson.GetBytes(body, "response_format.type").String())
			assert.True(t, gjson.GetBytes(body, "max_tokens").Exists())
		})
	}
}
