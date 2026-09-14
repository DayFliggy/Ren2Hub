package relay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayLegacyAdaptorResponseMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitTokenEncoders()
	for _, tc := range []struct {
		channelType int
		key         string
		jsonBody    string
		prefix      string
		terminal    string
		malformed   string
	}{
		{constant.ChannelTypeBaidu, "key", `{"id":"baidu_1","result":"matrix-response","usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`,
			"data: {\"result\":\"matrix-response\",\"is_end\":false}\n\n", "data: {\"result\":\"\",\"is_end\":true,\"usage\":{\"prompt_tokens\":10,\"total_tokens\":12}}\n\n", "data: {broken}\n\n"},
		{constant.ChannelTypeCohere, "key", `{"response_id":"cohere_1","text":"matrix-response","finish_reason":"COMPLETE","meta":{"billed_units":{"input_tokens":10,"output_tokens":2}}}`,
			"{\"event_type\":\"text-generation\",\"text\":\"matrix-response\",\"is_finished\":false}\n", "{\"event_type\":\"stream-end\",\"is_finished\":true,\"finish_reason\":\"COMPLETE\",\"response\":{\"meta\":{\"billed_units\":{\"input_tokens\":10,\"output_tokens\":2}}}}\n", "{broken}\n"},
		{constant.ChannelTypeTencent, "1|secret-id|secret-key", `{"Response":{"Id":"tencent_1","Choices":[{"Message":{"Content":"matrix-response","Role":"assistant"},"FinishReason":"stop"}],"Usage":{"PromptTokens":10,"CompletionTokens":2,"TotalTokens":12}}}`,
			"data: {\"Choices\":[{\"Delta\":{\"Content\":\"matrix-response\"}}]}\n\n", "data: {\"Choices\":[{\"Delta\":{\"Content\":\"\"},\"FinishReason\":\"stop\"}],\"Usage\":{\"PromptTokens\":10,\"CompletionTokens\":2,\"TotalTokens\":12}}\n\n", "data: {broken}\n\n"},
		{constant.ChannelTypeZhipu, "key", `{"success":true,"data":{"request_id":"zhipu_1","choices":[{"role":"assistant","content":"matrix-response"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}}`,
			"event: add\ndata: matrix-response\n\n", "event: finish\nmeta: {\"request_id\":\"zhipu_1\",\"task_status\":\"SUCCESS\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n", "meta: {broken}\n\n"},
		{constant.ChannelTypeDify, "key", `{"conversation_id":"dify_1","answer":"matrix-response","metadata":{"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}}`,
			"data: {\"event\":\"message\",\"answer\":\"matrix-response\"}\n\n", "data: {\"event\":\"message_end\",\"metadata\":{\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}}\n\n", "data: {broken}\n\n"},
	} {
		for _, mode := range []string{"json", "complete", "truncated", "malformed", "invalid_usage", "cancelled"} {
			t.Run(fmt.Sprintf("%s/%s", constant.GetChannelTypeName(tc.channelType), mode), func(t *testing.T) {
				stream := mode != "json"
				request, relayMode, path := gatewayMatrixRequest(types.RelayFormatOpenAI, "matrix-model", stream)
				apiType, ok := common.ChannelType2APIType(tc.channelType)
				require.True(t, ok)
				definition, ok := relaycapability.DescribeForCredentials(apiType, "matrix-model", []string{tc.key})
				require.True(t, ok)
				surface, ok := definition.Resolve(types.RelayFormatOpenAI)
				require.True(t, ok)
				if surface.TextProjection != nil && !slices.Contains(surface.TextProjection.Fields, "max_tokens") {
					request.(*dto.GeneralOpenAIRequest).MaxTokens = nil
				}
				info := &relaycommon.RelayInfo{Request: request, RelayMode: relayMode, RelayFormat: types.RelayFormatOpenAI, IsStream: stream, DisablePing: true, OriginModelName: "matrix-model", RequestURLPath: path,
					ChannelMeta: &relaycommon.ChannelMeta{ApiType: apiType, ChannelType: tc.channelType, UpstreamModelName: "matrix-model", ApiKey: tc.key}}
				info.SetEstimatePromptTokens(10)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, path, nil)
				common.SetContextKey(c, constant.ContextKeyChannelKey, tc.key)
				require.NoError(t, PlanRequestForChannel(c, info, &model.Channel{Id: 1, Type: tc.channelType, Key: tc.key}))
				adaptor := GetAdaptor(apiType)
				adaptor.Init(info)
				_, err := adaptor.ConvertOpenAIRequest(c, info, request.(*dto.GeneralOpenAIRequest))
				require.NoError(t, err)
				body := tc.jsonBody
				if stream {
					body = tc.prefix
				}
				switch mode {
				case "complete":
					body = strings.ReplaceAll(tc.prefix, "matrix-response", "matrix-") + strings.ReplaceAll(tc.prefix, "matrix-response", "response") + tc.terminal
				case "malformed":
					body += tc.malformed
				case "invalid_usage":
					body += strings.NewReplacer(`"total_tokens":12`, `"total_tokens":-12`, `"TotalTokens":12`, `"TotalTokens":-12`, `"output_tokens":2`, `"output_tokens":-2`).Replace(tc.terminal)
				case "cancelled":
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				}
				usage, apiErr := adaptor.DoResponse(c, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, info)
				if mode == "json" || mode == "complete" {
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					gatewayAssertClientResponse(t, recorder.Body.String(), types.RelayFormatOpenAI, stream)
					if stream {
						assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
						assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"role":"assistant"`), "the SDK must receive the role once across content deltas")
					}
				} else {
					require.NotNil(t, apiErr)
					assert.NotContains(t, recorder.Body.String(), "[DONE]")
					if mode != "cancelled" {
						assert.Contains(t, recorder.Body.String(), "matrix-response")
						if tokens, ok := usage.(*dto.Usage); assert.True(t, ok) && assert.NotNil(t, tokens) {
							assert.Greater(t, tokens.CompletionTokens, 0)
						}
					}
				}
				if stream {
					require.NotNil(t, info.StreamStatus)
					assert.Equal(t, mode == "complete", info.StreamStatus.IsNormalEnd())
				}
			})
		}
	}
}
