package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayAdaptorRequestMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for channelType := 1; channelType < constant.ChannelTypeDummy; channelType++ {
		apiType, found := common.ChannelType2APIType(channelType)
		if !found || GetAdaptor(apiType) == nil || apiType == constant.APITypeAdvancedCustom {
			continue
		}
		models := []string{"test-model"}
		if apiType == constant.APITypeVertexAi {
			models = []string{"claude-sonnet-4", "gemini-2.5-flash", "llama-3"}
		}
		if apiType == constant.APITypeAws {
			models = []string{"claude-sonnet-4", "nova-lite"}
		}
		for _, name := range models {
			definition, found := relaycapability.Describe(apiType, name)
			require.True(t, found)
			for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
				for _, stream := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/%s/stream=%t", constant.GetChannelTypeName(channelType), name, format, stream), func(t *testing.T) {
						request, mode, path := gatewayMatrixRequest(format, name, stream)
						surface, supported := definition.Resolve(format)
						if surface.TextProjection != nil && format == types.RelayFormatOpenAI && !slices.Contains(surface.TextProjection.Fields, "max_tokens") {
							request.(*dto.GeneralOpenAIRequest).MaxTokens = nil
						}
						info := &relaycommon.RelayInfo{Request: request, RelayFormat: format, RelayMode: mode, IsStream: stream, OriginModelName: name, RequestURLPath: path, ConversionPolicy: types.ConversionLossPolicySafe,
							ChannelMeta: &relaycommon.ChannelMeta{ApiType: apiType, ChannelType: channelType, UpstreamModelName: name, ChannelBaseUrl: "https://matrix.invalid", ApiKey: "matrix-test-key"}}
						if apiType == constant.APITypeXunfei {
							info.ApiKey = "test-app|test-secret|test-key"
						}
						c, _ := gin.CreateTestContext(httptest.NewRecorder())
						c.Request = httptest.NewRequest(http.MethodPost, path, nil)
						err := PlanRequestForChannel(c, info, &model.Channel{Id: 1, Type: channelType, Key: info.ApiKey})
						if !supported || (stream && !surface.Stream) {
							require.Error(t, err)
							assert.Nil(t, info.ConversionPlan)
							assert.False(t, c.Writer.Written())
							return
						}
						require.NoError(t, err)
						assert.Equal(t, surface.Target, info.ConversionPlan.To)
						adapter := GetAdaptor(apiType)
						adapter.Init(info)
						var value any
						switch req := request.(type) {
						case *dto.GeneralOpenAIRequest:
							value, err = adapter.ConvertOpenAIRequest(c, info, req)
						case *dto.OpenAIResponsesRequest:
							value, err = adapter.ConvertOpenAIResponsesRequest(c, info, *req)
						case *dto.ClaudeRequest:
							value, err = adapter.ConvertClaudeRequest(c, info, req)
						case *dto.GeminiChatRequest:
							value, err = adapter.ConvertGeminiRequest(c, info, req)
						}
						require.NoError(t, err)
						require.NotNil(t, value)
						body, err := common.Marshal(value)
						require.NoError(t, err)
						assert.Contains(t, string(body), "matrix-prompt")
						assert.False(t, c.Writer.Written(), "planning and conversion do not commit a client response")
					})
				}
			}
		}
	}
}

func gatewayMatrixRequest(format types.RelayFormat, model string, stream bool) (dto.Request, int, string) {
	maxTokens := uint(100)
	switch format {
	case types.RelayFormatOpenAI:
		return &dto.GeneralOpenAIRequest{Model: model, Stream: &stream, MaxTokens: &maxTokens, Messages: []dto.Message{{Role: "user", Content: "matrix-prompt"}}}, relayconstant.RelayModeChatCompletions, "/v1/chat/completions"
	case types.RelayFormatOpenAIResponses:
		return &dto.OpenAIResponsesRequest{Model: model, Stream: &stream, MaxOutputTokens: &maxTokens, Input: []byte(`"matrix-prompt"`)}, relayconstant.RelayModeResponses, "/v1/responses"
	case types.RelayFormatClaude:
		return &dto.ClaudeRequest{Model: model, Stream: &stream, MaxTokens: &maxTokens, Messages: []dto.ClaudeMessage{{Role: "user", Content: "matrix-prompt"}}}, relayconstant.RelayModeUnknown, "/v1/messages"
	default:
		path := "/v1beta/models/" + model + ":generateContent"
		if stream {
			path = "/v1beta/models/" + model + ":streamGenerateContent"
		}
		return &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "matrix-prompt"}}}}, GenerationConfig: dto.GeminiChatGenerationConfig{MaxOutputTokens: &maxTokens}}, relayconstant.RelayModeGemini, path
	}
}
