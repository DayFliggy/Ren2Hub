package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatJSONUsageValidation(t *testing.T) {
	for _, test := range []struct {
		name, usage        string
		prompt, completion int
		invalid, estimated bool
	}{
		{"provider", `,"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}`, 10, 2, false, false},
		{"total_omitted", `,"usage":{"prompt_tokens":10,"completion_tokens":2}`, 10, 2, false, false},
		{"explicit_zero_prompt", `,"usage":{"prompt_tokens":0,"completion_tokens":2,"total_tokens":2}`, 0, 2, false, false},
		{"explicit_zero_completion", `,"usage":{"prompt_tokens":10,"completion_tokens":0,"total_tokens":10}`, 10, 0, false, false},
		{"missing_usage", "", 17, 1, false, true},
		{"null_usage", `,"usage":null`, 17, 1, false, true},
		{"null_fields", `,"usage":{"prompt_tokens":null,"completion_tokens":null}`, 17, 1, false, true},
		{"missing_prompt", `,"usage":{"completion_tokens":2}`, 17, 2, false, true},
		{"missing_completion", `,"usage":{"prompt_tokens":10}`, 10, 1, false, true},
		{"negative_prompt", `,"usage":{"prompt_tokens":-1,"completion_tokens":2,"total_tokens":1}`, 0, 0, true, false},
		{"negative_completion", `,"usage":{"prompt_tokens":10,"completion_tokens":-1,"total_tokens":9}`, 0, 0, true, false},
		{"negative_total", `,"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":-1}`, 0, 0, true, false},
		{"contradictory_total", `,"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":11}`, 0, 0, true, false},
	} {
		for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
			t.Run(test.name+"/"+string(format), func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				info := &relaycommon.RelayInfo{RelayFormat: format, RelayMode: relayconstant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-4o-mini"}}
				info.SetEstimatePromptTokens(17)
				body := fmt.Sprintf(`{"id":"chat-fixture","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]%s}`, test.usage)
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
				usage, responseErr := OpenaiHandler(c, info, response)
				if test.invalid {
					require.NotNil(t, responseErr)
					assert.Equal(t, http.StatusBadGateway, responseErr.StatusCode)
					assert.Nil(t, usage)
					assert.Empty(t, recorder.Body.String())
					assert.False(t, c.Writer.Written())
					return
				}
				require.Nil(t, responseErr)
				require.NotNil(t, usage)
				assert.Equal(t, test.prompt, usage.PromptTokens)
				assert.Equal(t, test.completion, usage.CompletionTokens)
				assert.Equal(t, test.prompt+test.completion, usage.TotalTokens)
				if test.estimated {
					assert.Equal(t, "estimated", usage.UsageSource)
				} else {
					assert.Equal(t, "provider", usage.UsageSource)
				}
				if format == types.RelayFormatOpenAI {
					assert.EqualValues(t, usage.TotalTokens, gjson.GetBytes(recorder.Body.Bytes(), "usage.total_tokens").Int())
				}
				assert.Contains(t, recorder.Body.String(), "hello")
				assert.True(t, info.HasValidOutput())
			})
		}
	}
}

type chatJSONDeliveryWriter struct {
	*httptest.ResponseRecorder
	mode   string
	writes int
}

func (writer *chatJSONDeliveryWriter) Write(body []byte) (int, error) {
	writer.writes++
	switch writer.mode {
	case "short_write":
		return writer.ResponseRecorder.Write(body[:len(body)/2])
	case "write_error":
		return 0, io.ErrClosedPipe
	case "cancel_during_write":
		return 0, context.Canceled
	default:
		return writer.ResponseRecorder.Write(body)
	}
}

func TestChatJSONDeliveryFailureDoesNotBecomeSuccess(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, mode := range []string{"complete", "short_write", "write_error", "cancel_before_write", "cancel_during_write", "state_binding_failure"} {
			t.Run(string(format)+"/"+mode, func(t *testing.T) {
				writer := &chatJSONDeliveryWriter{ResponseRecorder: httptest.NewRecorder(), mode: mode}
				c, _ := gin.CreateTestContext(writer)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
				if mode == "cancel_before_write" {
					cancel()
				}
				if mode == "state_binding_failure" {
					c.Set("gateway_state_output_error", fmt.Errorf("state binding failed"))
				}
				info := &relaycommon.RelayInfo{RelayFormat: format, RelayMode: relayconstant.RelayModeChatCompletions, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-4o-mini"}}
				response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"chat-fixture","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))}
				usage, responseErr := OpenaiHandler(c, info, response)
				require.NotNil(t, usage)
				assert.Equal(t, 12, usage.TotalTokens)
				if mode == "complete" {
					require.Nil(t, responseErr)
					assert.True(t, info.HasValidOutput())
					assert.Equal(t, 1, writer.writes)
					return
				}
				require.NotNil(t, responseErr)
				assert.False(t, info.HasValidOutput())
				assert.True(t, types.IsSkipRetryError(responseErr))
				if mode == "state_binding_failure" {
					assert.Equal(t, types.ErrorCodeUpdateDataError, responseErr.GetErrorCode())
					assert.Nil(t, info.StreamStatus)
					assert.Zero(t, writer.writes)
					return
				}
				require.NotNil(t, info.StreamStatus)
				assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
				if mode == "cancel_before_write" {
					assert.Zero(t, writer.writes)
				} else {
					assert.Equal(t, 1, writer.writes)
				}
				if mode == "short_write" {
					assert.ErrorIs(t, info.StreamStatus.Err(), io.ErrShortWrite)
				} else if mode == "write_error" {
					assert.ErrorIs(t, info.StreamStatus.Err(), io.ErrClosedPipe)
				} else {
					assert.ErrorIs(t, info.StreamStatus.Err(), context.Canceled)
				}
			})
		}
	}
}
