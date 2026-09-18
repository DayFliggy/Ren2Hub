package claude

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudePauseNeverBecomesConvertedSuccess(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
		for _, stream := range []bool{false, true} {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{RelayFormat: format, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			body := `{"id":"msg_pause","type":"message","model":"claude-test","stop_reason":"pause_turn","content":[],"usage":{"input_tokens":3,"output_tokens":5}}`
			if stream {
				body = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_pause\",\"usage\":{\"input_tokens\":3}}}\n\n" +
					"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"pause_turn\"},\"usage\":{\"output_tokens\":5}}\n\n" +
					"data: {\"type\":\"message_stop\"}\n\n"
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			handler := ClaudeHandler
			if stream {
				handler = ClaudeStreamHandler
			}
			usage, apiErr := handler(c, resp, info)
			require.NotNil(t, usage)
			assert.Equal(t, 3, usage.PromptTokens)
			assert.Equal(t, 5, usage.CompletionTokens)
			if format == types.RelayFormatClaude {
				require.Nil(t, apiErr)
				assert.Contains(t, recorder.Body.String(), "pause_turn")
			} else {
				require.NotNil(t, apiErr)
				assert.Equal(t, helper.ResponseConversionErrorCode, apiErr.GetErrorCode())
				assert.True(t, types.IsSkipRetryError(apiErr))
				assert.NotContains(t, recorder.Body.String(), "completed")
				assert.NotContains(t, recorder.Body.String(), "[DONE]")
			}
		}
	}
}

type cancelClaudeOutputWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *cancelClaudeOutputWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if strings.Contains(string(data), "delivered response") {
		w.cancel()
	}
	return n, err
}

type heldClaudeStreamBody struct {
	io.Reader
	closed chan struct{}
	once   sync.Once
}

func (b *heldClaudeStreamBody) Read(data []byte) (int, error) {
	n, err := b.Reader.Read(data)
	if err == io.EOF {
		<-b.closed
	}
	return n, err
}

func (b *heldClaudeStreamBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func TestClaudeStreamFinalBillingMatchesDeliveredUsage(t *testing.T) {
	for _, ending := range []string{"normal", "missing_usage", "eof", "cancel"} {
		t.Run(ending, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			body := "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0,\"cache_read_input_tokens\":3,\"cache_creation_input_tokens\":7,\"cache_creation\":{\"ephemeral_5m_input_tokens\":2,\"ephemeral_1h_input_tokens\":5}}}}\n\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Already delivered response text with several words.\"}}\n\n"
			if ending == "normal" {
				body += "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":12}}\n\n" +
					"data: {\"type\":\"message_stop\"}\n\n"
			} else if ending == "missing_usage" {
				body += "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"}}\n\n" +
					"data: {\"type\":\"message_stop\"}\n\n"
			}
			var upstreamBody io.ReadCloser = io.NopCloser(strings.NewReader(body))
			if ending == "cancel" {
				ctx, cancel := context.WithCancel(c.Request.Context())
				t.Cleanup(cancel)
				c.Request = c.Request.WithContext(ctx)
				c.Writer = &cancelClaudeOutputWriter{ResponseWriter: c.Writer, cancel: cancel}
				upstreamBody = &heldClaudeStreamBody{Reader: strings.NewReader(body), closed: make(chan struct{})}
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: upstreamBody}
			usage, apiErr := ClaudeStreamHandler(c, resp, info)
			if ending == "normal" || ending == "missing_usage" {
				require.Nil(t, apiErr)
				if ending == "normal" {
					assert.Equal(t, 12, usage.CompletionTokens)
				}
			} else {
				require.NotNil(t, apiErr)
				assert.NotContains(t, recorder.Body.String(), "message_stop")
			}
			require.Contains(t, recorder.Body.String(), "delivered response")
			require.NotNil(t, usage)
			require.Positive(t, usage.CompletionTokens)
			require.NotNil(t, usage.BillingUsage)
			require.NotNil(t, usage.BillingUsage.ClaudeUsage)
			billed := usage.BillingUsage.ClaudeUsage
			assert.Equal(t, usage.CompletionTokens, billed.OutputTokens)
			assert.Equal(t, 10, billed.InputTokens)
			assert.Equal(t, 3, billed.CacheReadInputTokens)
			assert.Equal(t, 7, billed.CacheCreationInputTokens)
			assert.Equal(t, 2, billed.GetCacheCreation5mTokens())
			assert.Equal(t, 5, billed.GetCacheCreation1hTokens())
			assert.Equal(t, ending != "normal", usage.BillingUsage.Estimated)
			if ending == "cancel" {
				assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
			}
		})
	}
}

func TestClaudeChatStreamReconstructsToolsAcrossTextBlocks(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
	state := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	for _, event := range []string{
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"checking"}}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"first","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"q\":1}"}}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"text","text":"next"}}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"second","name":"lookup","input":{}}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"q\":2}"}}`,
	} {
		require.Nil(t, HandleStreamResponseData(c, info, state, event))
	}
	tools := map[int]dto.ToolCallResponse{}
	for _, line := range strings.Split(recorder.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var chunk dto.ChatCompletionsStreamResponse
		require.NoError(t, common.UnmarshalJsonStr(strings.TrimPrefix(line, "data: "), &chunk))
		for _, delta := range chunk.Choices[0].Delta.ToolCalls {
			require.NotNil(t, delta.Index)
			tool := tools[*delta.Index]
			tool.ID += delta.ID
			tool.Function.Name += delta.Function.Name
			tool.Function.Arguments += delta.Function.Arguments
			tools[*delta.Index] = tool
		}
	}
	require.Len(t, tools, 2)
	assert.Equal(t, "first", tools[0].ID)
	assert.Equal(t, "second", tools[1].ID)
	assert.Equal(t, "lookup", tools[0].Function.Name)
	assert.Equal(t, "lookup", tools[1].Function.Name)
	assert.JSONEq(t, `{"q":1}`, tools[0].Function.Arguments)
	assert.JSONEq(t, `{"q":2}`, tools[1].Function.Arguments)
}
