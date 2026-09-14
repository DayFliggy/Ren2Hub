package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOllamaChatHandlerNonStreamToolCalls(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name   string
		raw    string
		wantID string
	}{
		{
			name:   "compact json per-line parse path",
			raw:    `{"model":"llama3.1","created_at":"2026-05-27T12:00:00Z","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_upstream","function":{"name":"get_weather","arguments":{"city":"Paris","days":0}}}]},"done":true,"done_reason":"stop","prompt_eval_count":5,"eval_count":7}`,
			wantID: "call_upstream",
		},
		{
			name: "pretty json fallback parse path",
			raw: `{
  "model": "llama3.1",
  "created_at": "2026-05-27T12:00:00Z",
  "message": {
    "role": "assistant",
    "content": "",
    "tool_calls": [
      {
        "function": {
          "name": "get_weather",
          "arguments": {
            "city": "Paris",
            "days": 0
          }
        }
      }
    ]
  },
  "done": true,
  "done_reason": "stop",
  "prompt_eval_count": 5,
  "eval_count": 7
}`,
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(tt.raw)),
			}

			usage, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "fallback-model"},
			}, resp)
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 12, usage.TotalTokens)

			var out dto.OpenAITextResponse
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &out))
			require.Len(t, out.Choices, 1)
			assert.Equal(t, constant.FinishReasonToolCalls, out.Choices[0].FinishReason)

			var toolCalls []dto.ToolCallResponse
			require.NoError(t, common.Unmarshal(out.Choices[0].Message.ToolCalls, &toolCalls))
			require.Len(t, toolCalls, 1)
			if tt.wantID != "" {
				assert.Equal(t, tt.wantID, toolCalls[0].ID)
			} else {
				assert.Equal(t, "call_"+out.Id+"_0", toolCalls[0].ID)
			}
			assert.Equal(t, "function", toolCalls[0].Type)
			assert.Equal(t, "get_weather", toolCalls[0].Function.Name)
			assert.Nil(t, toolCalls[0].Index)

			var args map[string]any
			require.NoError(t, common.Unmarshal([]byte(toolCalls[0].Function.Arguments), &args))
			assert.Equal(t, "Paris", args["city"])
			assert.Equal(t, float64(0), args["days"])
		})
	}
}

func TestOllamaToolResponsesCanContinueAcrossMultipleRounds(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{Messages: []dto.Message{{Role: "user", Content: "look it up"}}}
	var previousID string
	for range 2 {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":{"integer":9007199254740993,"zero":0,"disabled":false}}}]},"done":true}`))}
		_, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}}, resp)
		require.Nil(t, apiErr)
		var response dto.OpenAITextResponse
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		require.Len(t, response.Choices, 1)
		message := response.Choices[0].Message
		calls := message.ParseToolCalls()
		require.Len(t, calls, 1)
		assert.Contains(t, calls[0].Function.Arguments, "9007199254740993")
		assert.NotEqual(t, previousID, calls[0].ID, "synthetic call IDs must be unique across rounds")
		previousID = calls[0].ID
		request.Messages = append(request.Messages, message, dto.Message{Role: "tool", ToolCallId: calls[0].ID, Content: "result"})
		_, err := openAIChatToOllamaChat(c, request)
		require.NoError(t, err)
	}
}

func TestOllamaRejectsMalformedToolResponsesBeforeClientCommit(t *testing.T) {
	for _, input := range []string{
		`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":null}}]},"done":true}`,
		`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"lookup","arguments":[]}}]},"done":true}`,
		`{"message":{"role":"assistant","tool_calls":[{"function":{"name":"","arguments":{}}}]},"done":true}`,
	} {
		for _, stream := range []bool{false, true} {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(input + "\n"))}
			_, apiErr := (&Adaptor{}).DoResponse(c, resp, &relaycommon.RelayInfo{IsStream: stream, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}})
			require.NotNil(t, apiErr)
			assert.Empty(t, w.Body.String())
			assert.False(t, c.Writer.Written())
		}
	}
}

func TestOllamaClaudeJSONKeepsToolIdentity(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	response := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"model":"llama3.1","message":{"role":"assistant","content":"","tool_calls":[{"id":"upstream-call","function":{"name":"lookup","arguments":{"n":0}}}]},"done":true,"prompt_eval_count":5,"eval_count":7}`))}
	usage, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}}, response)
	require.Nil(t, apiErr)
	assert.Equal(t, 12, usage.TotalTokens)
	var body map[string]any
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "message", body["type"])
	assert.Equal(t, "tool_use", body["stop_reason"])
	assert.NotContains(t, body, "choices")
	content := body["content"].([]any)
	require.Len(t, content, 1)
	assert.Equal(t, "upstream-call", content[0].(map[string]any)["id"])
}

func TestOllamaStreamHasExplicitCompletionAndPreludeBoundary(t *testing.T) {
	content := `{"message":{"role":"assistant","content":"hello"},"done":false}` + "\n"
	done := `{"done":true,"prompt_eval_count":5,"eval_count":7}` + "\n"
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		for _, test := range []struct {
			name, input         string
			complete, delivered bool
		}{
			{"complete", content + done, true, true},
			{"truncated", content, false, true},
			{"malformed", content + "invalid\n", false, true},
			{"prelude_only", `{"message":{"role":"assistant"},"done":false}` + "\n", false, false},
			{"prelude_limit", strings.Repeat(`{"message":{"role":"assistant"},"done":false}`+"\n", 65), false, false},
		} {
			t.Run(string(format)+"/"+test.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{RelayFormat: format, IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}}
				resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(test.input))}
				usage, apiErr := ollamaStreamHandler(c, info, resp)
				assert.Equal(t, test.delivered, info.HasValidOutput())
				assert.Equal(t, test.complete, info.StreamStatus.IsNormalEnd())
				if test.complete {
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					assert.Equal(t, 12, usage.TotalTokens)
					assert.Equal(t, "provider", usage.UsageSource)
				} else {
					require.NotNil(t, apiErr)
					assert.NotContains(t, w.Body.String(), "[DONE]")
					assert.NotContains(t, w.Body.String(), "message_stop")
					if test.delivered {
						require.NotNil(t, usage)
						assert.Equal(t, "estimated", usage.UsageSource)
					} else {
						assert.Empty(t, w.Body.String())
					}
				}
				if test.complete && format == types.RelayFormatClaude {
					assert.Contains(t, w.Body.String(), "message_start")
					assert.Equal(t, 1, strings.Count(w.Body.String(), "event: message_stop"))
					assert.NotContains(t, w.Body.String(), "chat.completion.chunk")
				}
			})
		}
	}
}

func TestOllamaJSONRejectsIncompleteOrCorruptCompletion(t *testing.T) {
	for _, input := range []string{
		`{"message":{"content":"hello"},"done":false}`,
		`{"message":{"content":"hello"}}` + "\ninvalid\n" + `{"done":true}`,
		`{"done":true,"eval_count":-1}`,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(input))}
		_, apiErr := ollamaChatHandler(c, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama"}}, resp)
		require.NotNil(t, apiErr)
		assert.Empty(t, w.Body.String())
	}
}
