package relay

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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
	"github.com/tidwall/gjson"
)

// The fixture is a provider wire response passed through the real adaptor after
// request conversion. This covers host wiring, not provider authentication.
func TestGatewayStandardAdaptorResponseMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitTokenEncoders()
	cases := 0
	for channelType := 1; channelType < constant.ChannelTypeDummy; channelType++ {
		apiType, found := common.ChannelType2APIType(channelType)
		if !found {
			continue
		}
		switch apiType {
		case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference,
			constant.APITypeAnthropic, constant.APITypeGemini, constant.APITypeVertexAi,
			constant.APITypeNewAPI, constant.APITypeSub2API,
			constant.APITypeDeepSeek, constant.APITypeAli, constant.APITypeMoonshot,
			constant.APITypeMiniMax, constant.APITypeSiliconFlow, constant.APITypeVolcEngine,
			constant.APITypePerplexity, constant.APITypeXai, constant.APITypeBaiduV2,
			constant.APITypeMistral, constant.APITypeZhipuV4, constant.APITypeCloudflare, constant.APITypeSubmodel, constant.APITypeTencent:
		default:
			continue
		}
		models := []string{"matrix-model"}
		if apiType == constant.APITypeVertexAi {
			models = []string{"claude-sonnet-4", "gemini-2.5-flash", "llama-3"}
		}
		for _, name := range models {
			definition, found := relaycapability.Describe(apiType, name)
			require.True(t, found)
			for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
				surface, supported := definition.Resolve(format)
				if !supported {
					continue
				}
				for _, stream := range []bool{false, true} {
					if stream && !surface.Stream {
						continue
					}
					cases++
					t.Run(fmt.Sprintf("%s/%s/%s/stream=%t", constant.GetChannelTypeName(channelType), name, format, stream), func(t *testing.T) {
						request, mode, path := gatewayMatrixRequest(format, name, stream)
						info := &relaycommon.RelayInfo{Request: request, RelayFormat: format, RelayMode: mode, IsStream: stream, OriginModelName: name, RequestURLPath: path, ConversionPolicy: types.ConversionLossPolicySafe,
							ChannelMeta: &relaycommon.ChannelMeta{ApiType: apiType, ChannelType: channelType, UpstreamModelName: name, ChannelBaseUrl: "https://matrix.invalid", ApiKey: "matrix-test-key"}}
						info.SetEstimatePromptTokens(10)
						recorder := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(recorder)
						c.Request = httptest.NewRequest(http.MethodPost, path, nil)
						require.NoError(t, PlanRequestForChannel(c, info, &model.Channel{Id: 1, Type: channelType, Key: info.ApiKey}))
						adapter := GetAdaptor(apiType)
						adapter.Init(info)
						var err error
						switch req := request.(type) {
						case *dto.GeneralOpenAIRequest:
							_, err = adapter.ConvertOpenAIRequest(c, info, req)
						case *dto.OpenAIResponsesRequest:
							_, err = adapter.ConvertOpenAIResponsesRequest(c, info, *req)
						case *dto.ClaudeRequest:
							_, err = adapter.ConvertClaudeRequest(c, info, req)
						case *dto.GeminiChatRequest:
							_, err = adapter.ConvertGeminiRequest(c, info, req)
						}
						require.NoError(t, err)
						body := gatewayStandardWireResponse(t, info.ConversionPlan.To, stream)
						contentType := "application/json"
						if stream {
							contentType = "text/event-stream"
						}
						usage, responseErr := adapter.DoResponse(c, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body))}, info)
						require.Nil(t, responseErr)
						assert.NotNil(t, usage)
						if stream {
							require.NotNil(t, info.StreamStatus)
							assert.True(t, info.StreamStatus.IsNormalEnd(), "%+v", info.StreamStatus)
						}
						gatewayAssertClientResponse(t, recorder.Body.String(), format, stream)
					})
				}
			}
		}
	}
	t.Logf("%d actual adaptor JSON/SSE response paths checked", cases)
}

func gatewayStandardWireResponse(t *testing.T, format types.RelayFormat, stream bool) string {
	t.Helper()
	var response any
	var events []any
	switch format {
	case types.RelayFormatOpenAI:
		response = map[string]any{"id": "chat_matrix", "object": "chat.completion", "model": "matrix-model", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "matrix-response"}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}}
		events = []any{
			map[string]any{"id": "chat_matrix", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "content": "matrix-response"}, "finish_reason": nil}}},
			map[string]any{"id": "chat_matrix", "object": "chat.completion.chunk", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}},
		}
	case types.RelayFormatClaude:
		response = map[string]any{"id": "msg_matrix", "type": "message", "model": "matrix-model", "role": "assistant", "content": []any{map[string]string{"type": "text", "text": "matrix-response"}}, "stop_reason": "end_turn", "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}}
		events = []any{
			map[string]any{"type": "message_start", "message": map[string]any{"id": "msg_matrix", "type": "message", "role": "assistant", "content": []any{}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 0}}},
			map[string]any{"type": "content_block_start", "index": 0, "content_block": map[string]string{"type": "text", "text": ""}},
			map[string]any{"type": "content_block_delta", "index": 0, "delta": map[string]string{"type": "text_delta", "text": "matrix-response"}},
			map[string]any{"type": "content_block_stop", "index": 0},
			map[string]any{"type": "message_delta", "delta": map[string]string{"stop_reason": "end_turn"}, "usage": map[string]int{"output_tokens": 2}},
			map[string]any{"type": "message_stop"},
		}
	case types.RelayFormatGemini:
		response = map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{map[string]string{"text": "matrix-response"}}}, "finishReason": "STOP"}}, "usageMetadata": map[string]int{"promptTokenCount": 10, "candidatesTokenCount": 2, "totalTokenCount": 12}}
		events = []any{response}
	case types.RelayFormatOpenAIResponses:
		part := map[string]any{"type": "output_text", "text": "matrix-response", "annotations": []any{}}
		item := map[string]any{"id": "msg_matrix", "type": "message", "role": "assistant", "status": "completed", "content": []any{part}}
		response = map[string]any{"id": "resp_matrix", "object": "response", "status": "completed", "output": []any{item}, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}
		events = []any{
			map[string]any{"type": "response.created", "sequence_number": 0, "response": map[string]any{"id": "resp_matrix", "object": "response", "status": "in_progress", "output": []any{}}},
			map[string]any{"type": "response.output_item.added", "sequence_number": 1, "output_index": 0, "item": map[string]any{"id": "msg_matrix", "type": "message", "role": "assistant", "status": "in_progress", "content": []any{}}},
			map[string]any{"type": "response.content_part.added", "sequence_number": 2, "output_index": 0, "content_index": 0, "item_id": "msg_matrix", "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}},
			map[string]any{"type": "response.output_text.delta", "sequence_number": 3, "output_index": 0, "content_index": 0, "item_id": "msg_matrix", "delta": "matrix-response"},
			map[string]any{"type": "response.output_text.done", "sequence_number": 4, "output_index": 0, "content_index": 0, "item_id": "msg_matrix", "text": "matrix-response"},
			map[string]any{"type": "response.content_part.done", "sequence_number": 5, "output_index": 0, "content_index": 0, "item_id": "msg_matrix", "part": part},
			map[string]any{"type": "response.output_item.done", "sequence_number": 6, "output_index": 0, "item": item},
			map[string]any{"type": "response.completed", "sequence_number": 7, "response": response},
		}
	default:
		t.Fatalf("no standard response fixture for %s", format)
	}
	if !stream {
		body, err := common.Marshal(response)
		require.NoError(t, err)
		return string(body)
	}
	var body strings.Builder
	for _, event := range events {
		encoded, err := common.Marshal(event)
		require.NoError(t, err)
		if eventType := gjson.GetBytes(encoded, "type").String(); eventType != "" {
			fmt.Fprintf(&body, "event: %s\n", eventType)
		}
		fmt.Fprintf(&body, "data: %s\n\n", encoded)
	}
	if format == types.RelayFormatOpenAI {
		body.WriteString("data: [DONE]\n\n")
	}
	return body.String()
}

func gatewayAssertClientResponse(t *testing.T, body string, format types.RelayFormat, stream bool) {
	t.Helper()
	paths := map[types.RelayFormat][2]string{
		types.RelayFormatOpenAI:          {"choices.0.message.content", "choices.0.finish_reason"},
		types.RelayFormatOpenAIResponses: {"output.0.content.0.text", "status"},
		types.RelayFormatClaude:          {"content.0.text", "stop_reason"},
		types.RelayFormatGemini:          {"candidates.0.content.parts.0.text", "candidates.0.finishReason"},
	}
	if !stream {
		require.True(t, gjson.Valid(body), body)
		assert.Equal(t, "matrix-response", gjson.Get(body, paths[format][0]).String(), body)
		assert.NotEmpty(t, gjson.Get(body, paths[format][1]).String(), body)
		return
	}
	var text strings.Builder
	finished := false
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		require.True(t, gjson.Valid(data), data)
		switch format {
		case types.RelayFormatOpenAI:
			text.WriteString(gjson.Get(data, "choices.0.delta.content").String())
			finished = finished || gjson.Get(data, "choices.0.finish_reason").String() != ""
		case types.RelayFormatClaude:
			if gjson.Get(data, "type").String() == "content_block_delta" {
				text.WriteString(gjson.Get(data, "delta.text").String())
			}
			finished = finished || gjson.Get(data, "type").String() == "message_stop"
		case types.RelayFormatOpenAIResponses:
			if gjson.Get(data, "type").String() == "response.output_text.delta" {
				text.WriteString(gjson.Get(data, "delta").String())
			}
			finished = finished || gjson.Get(data, "type").String() == "response.completed"
		case types.RelayFormatGemini:
			text.WriteString(gjson.Get(data, "candidates.0.content.parts.0.text").String())
			finished = finished || gjson.Get(data, "candidates.0.finishReason").String() != ""
		}
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, "matrix-response", text.String(), body)
	assert.True(t, finished, body)
}
