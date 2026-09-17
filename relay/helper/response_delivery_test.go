package helper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type eventDeliveryWriter struct {
	*httptest.ResponseRecorder
	shortWrite bool
	cancel     context.CancelFunc
}

func TestSSEWritersPreserveFailures(t *testing.T) {
	for _, api := range []string{"claude", "claude_chunk", "string", "done"} {
		t.Run(api, func(t *testing.T) {
			writer := &eventDeliveryWriter{ResponseRecorder: httptest.NewRecorder(), shortWrite: true}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil)
			var err error
			switch api {
			case "claude":
				err = ClaudeData(c, dto.ClaudeResponse{Type: "message_stop"})
			case "claude_chunk":
				ClaudeChunkData(c, dto.ClaudeResponse{Type: "message_stop"}, `{"type":"message_stop"}`)
			case "string":
				err = StringData(c, `{"text":"hello"}`)
			case "done":
				Done(c)
			}
			if api == "claude" || api == "string" {
				require.ErrorIs(t, err, io.ErrShortWrite)
			}
			require.ErrorIs(t, StreamOutputError(c), io.ErrShortWrite)
			assert.ErrorIs(t, StreamOutputError(c), ErrStreamClientWrite)
			written := writer.Body.String()
			err = StringData(c, "[DONE]")
			require.ErrorIs(t, err, io.ErrShortWrite)
			assert.Equal(t, written, writer.Body.String(), "an ignored error must prevent subsequent terminal writes")
		})
	}
}

func TestSSEWritersRejectBeforeCommit(t *testing.T) {
	for _, failure := range []string{"state_binding", "marshal", "event_type", "cancelled"} {
		t.Run(failure, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil)
			var err error
			switch failure {
			case "state_binding":
				c.Set("gateway_state_output", "invalid state context")
				ClaudeChunkData(c, dto.ClaudeResponse{Type: "message_stop"}, `{"type":"message_stop"}`)
				err = StreamOutputError(c)
				value, _ := c.Get(service.GatewayStateOutputErrorKey)
				assert.Same(t, value, err)
			case "marshal":
				err = ClaudeData(c, dto.ClaudeResponse{Type: "content_block_start", ContentBlock: &dto.ClaudeMediaMessage{Input: make(chan int)}})
			case "event_type":
				err = ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "message\n\ndata: [DONE]"}, "{}")
			case "cancelled":
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
				err = ClaudeData(c, dto.ClaudeResponse{Type: "message_stop"})
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Error(t, err)
			assert.ErrorIs(t, StreamOutputError(c), err)
			assert.Equal(t, failure == "cancelled", strings.Contains(err.Error(), ErrStreamClientWrite.Error()))
			assert.Empty(t, recorder.Body.String())
			assert.False(t, c.Writer.Written())
		})
	}
}

func TestSSEDataNewlinesCannotCreateEvents(t *testing.T) {
	for _, data := range []string{
		"{\n  \"text\": \"hello\",\n  \"value\": 0\n}",
		"first\n\nevent: injected\ndata: [DONE]",
		"first\r\n\r\nevent: injected\rdata: [DONE]\r",
	} {
		t.Run(data, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/stream", nil)
			delivered, err := WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: "content_block_delta"}, data)
			require.NoError(t, err)
			assert.True(t, delivered)
			frame := recorder.Body.String()
			assert.Equal(t, 1, strings.Count(frame, "\n\n"), "only the final blank line may end an event")
			assert.Equal(t, 1, strings.Count(frame, "event: content_block_delta\n"))
			assert.NotContains(t, frame, "\nevent: injected")
			lines := strings.Split(strings.TrimSuffix(frame, "\n\n"), "\n")
			var values []string
			for _, line := range lines[1:] {
				require.True(t, strings.HasPrefix(line, "data: "), line)
				values = append(values, strings.TrimPrefix(line, "data: "))
			}
			normalized := strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n")
			assert.Equal(t, normalized, strings.Join(values, "\n"))
			if strings.HasPrefix(data, "{") {
				var parsed map[string]any
				require.NoError(t, common.UnmarshalJsonStr(strings.Join(values, "\n"), &parsed))
				assert.Equal(t, "hello", parsed["text"])
			}
		})
	}
}

func TestIgnoredClaudeWriteFailureStopsScannerAndCanResetForRetry(t *testing.T) {
	c, resp, info := setupStreamTest(t, strings.NewReader("data: {\"type\":\"message_start\"}\n\ndata: [DONE]\n"))
	c.Set("gateway_state_output", "invalid state context")
	callbacks := 0
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		callbacks++
		ClaudeChunkData(c, dto.ClaudeResponse{Type: "message_start"}, data)
	})
	assert.Equal(t, 1, callbacks)
	require.NotNil(t, StreamError(info))
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.False(t, info.HasValidOutput())
	assert.False(t, c.Writer.Written())
	require.Error(t, StreamOutputError(c))

	// The logical request consumes the first attempt's error before resetting
	// the new attempt. Its prelude was never exposed to the client.
	c.Set(service.GatewayStateOutputErrorKey, nil)
	c.Set("gateway_state_output", nil)
	ResetStreamOutputError(c)
	resp.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"message_stop\"}\n"))
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		ClaudeChunkData(c, dto.ClaudeResponse{Type: "message_stop"}, data)
	})
	require.Nil(t, StreamError(info))
	assert.NoError(t, StreamOutputError(c))
	assert.True(t, c.Writer.Written())
}

func (w *eventDeliveryWriter) Write(body []byte) (int, error) {
	if w.shortWrite {
		return w.ResponseRecorder.Write(body[:len(body)-1])
	}
	n, err := w.ResponseRecorder.Write(body)
	if w.cancel != nil {
		w.cancel()
	}
	return n, err
}

func (w *eventDeliveryWriter) WriteString(body string) (int, error) {
	return w.Write([]byte(body))
}

func TestResponseEventDeliveryBoundary(t *testing.T) {
	for _, scenario := range []string{"complete", "short_write", "cancel_after_complete", "uncommitted_prelude"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			writer := &eventDeliveryWriter{ResponseRecorder: httptest.NewRecorder(), shortWrite: scenario == "short_write"}
			if scenario == "cancel_after_complete" {
				writer.cancel = cancel
			}
			c, _ := gin.CreateTestContext(writer)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil).WithContext(ctx)
			if scenario == "uncommitted_prelude" {
				c.Writer = &StreamPreludeWriter{ResponseWriter: c.Writer}
			}
			delivered, err := WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: "image_generation.completed"}, `{"b64_json":"image"}`)
			assert.Equal(t, scenario == "complete" || scenario == "cancel_after_complete", delivered)
			switch scenario {
			case "short_write":
				require.ErrorIs(t, err, io.ErrShortWrite)
			case "cancel_after_complete":
				require.ErrorIs(t, err, context.Canceled)
			default:
				require.NoError(t, err)
			}
			if scenario == "uncommitted_prelude" {
				assert.Empty(t, writer.Body.String())
			} else if scenario != "short_write" {
				assert.Equal(t, "event: image_generation.completed\ndata: {\"b64_json\":\"image\"}\n\n", writer.Body.String())
			}
		})
	}
}

func TestPreludeShortWriteDoesNotBecomeValidOutput(t *testing.T) {
	c, response, info := setupStreamTest(t, strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: [DONE]\n\n"))
	writer := &eventDeliveryWriter{ResponseRecorder: httptest.NewRecorder(), shortWrite: true}
	contextWithWriter, _ := gin.CreateTestContext(writer)
	c.Writer = contextWithWriter.Writer
	StreamScannerHandler(c, response, info, func(data string, result *StreamResult) {
		_ = StringData(c, data)
	})
	require.ErrorIs(t, info.StreamStatus.EndError, io.ErrShortWrite)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	assert.False(t, info.HasValidOutput())
	assert.NotContains(t, writer.Body.String(), "[DONE]")
}
