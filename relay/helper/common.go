package helper

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

const streamOutputErrorKey = "relay_stream_output_error"

// ErrStreamClientWrite distinguishes downstream write/flush failures from
// provider parsing and gateway state-binding failures.
var ErrStreamClientWrite = errors.New("client stream write failed")

// StreamOutputError preserves failures for legacy callers whose public APIs
// have no error return, including terminal writes after the scanner returns.
func StreamOutputError(c *gin.Context) error {
	if c == nil {
		return nil
	}
	value, _ := c.Get(streamOutputErrorKey)
	err, _ := value.(error)
	return err
}

// ResetStreamOutputError starts a new upstream attempt. A previous attempt's
// failure must remain visible until the logical request has recorded it.
func ResetStreamOutputError(c *gin.Context) {
	if c != nil {
		c.Set(streamOutputErrorKey, nil)
	}
}

func recordStreamOutputError(c *gin.Context, err error) error {
	if c == nil || err == nil {
		return err
	}
	if first := StreamOutputError(c); first != nil {
		err = first
	} else {
		c.Set(streamOutputErrorKey, err)
	}
	// The scanner already observes this failure field after every callback.
	// This also stops legacy callbacks that ignore an error return.
	if writer, ok := c.Writer.(*StreamPreludeWriter); ok && writer.err == nil {
		writer.err = err
	}
	return err
}

func FlushWriter(c *gin.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("flush panic recovered: %v", r)
		}
	}()

	if c == nil || c.Writer == nil {
		return nil
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return errors.New("streaming error: flusher not found")
	}

	flusher.Flush()
	return nil
}

func requestContextDone(c *gin.Context) bool {
	return c != nil && c.Request != nil && c.Request.Context().Err() != nil
}

func SetEventStreamHeaders(c *gin.Context) {
	// 检查是否已经设置过头部
	if _, exists := c.Get("event_stream_headers_set"); exists {
		return
	}

	// 设置标志，表示头部已经设置过
	c.Set("event_stream_headers_set", true)

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Transfer-Encoding", "chunked")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
}

func ClaudeData(c *gin.Context, resp dto.ClaudeResponse) error {
	jsonData, err := common.Marshal(resp)
	if err != nil {
		return recordStreamOutputError(c, fmt.Errorf("error marshalling stream response: %w", err))
	}
	_, err = WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: resp.Type}, string(jsonData))
	return err
}

func ClaudeChunkData(c *gin.Context, resp dto.ClaudeResponse, data string) {
	// WriteResponseChunkData records any failure for the scanner and finalizer.
	_, _ = WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: resp.Type}, data)
}

func ResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) error {
	_, err := WriteResponseChunkData(c, resp, data)
	return err
}

// WriteResponseChunkData separates a complete event write from a later flush or
// cancellation failure. Buffered prelude bytes do not count as client delivery.
func WriteResponseChunkData(c *gin.Context, resp dto.ResponsesStreamResponse, data string) (bool, error) {
	if c == nil || c.Writer == nil {
		return false, recordStreamOutputError(c, errors.New("context or writer is nil"))
	}
	if err := StreamOutputError(c); err != nil {
		return false, recordStreamOutputError(c, err)
	}
	if requestContextDone(c) {
		return false, recordStreamOutputError(c, fmt.Errorf("%w: %w", ErrStreamClientWrite, c.Request.Context().Err()))
	}
	if strings.ContainsAny(resp.Type, "\r\n") {
		return false, recordStreamOutputError(c, errors.New("invalid SSE event type"))
	}
	if err := service.BindGatewayOutput(c, []byte(data)); err != nil {
		return false, recordStreamOutputError(c, err)
	}
	// Each payload line belongs to the same SSE data field. Prefixing every
	// line prevents embedded newlines from creating events or terminal frames.
	data = strings.ReplaceAll(strings.ReplaceAll(data, "\r\n", "\n"), "\r", "\n")
	frame := "data: " + strings.ReplaceAll(data, "\n", "\ndata: ") + "\n\n"
	if resp.Type != "" {
		frame = "event: " + resp.Type + "\n" + frame
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	prelude, buffered := c.Writer.(*StreamPreludeWriter)
	buffered = buffered && !prelude.committed
	n, err := c.Writer.WriteString(frame)
	delivered := n == len(frame) && c.Writer.Written()
	if err == nil && n != len(frame) {
		err = io.ErrShortWrite
	}
	if err != nil {
		if buffered {
			return delivered, recordStreamOutputError(c, err)
		}
		return delivered, recordStreamOutputError(c, fmt.Errorf("%w: %w", ErrStreamClientWrite, err))
	}
	if err := FlushWriter(c); err != nil {
		return delivered, recordStreamOutputError(c, fmt.Errorf("%w: %w", ErrStreamClientWrite, err))
	}
	return delivered, nil
}

func StringData(c *gin.Context, str string) error {
	_, err := WriteResponseChunkData(c, dto.ResponsesStreamResponse{}, str)
	return err
}

func PingData(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return errors.New("context or writer is nil")
	}

	if requestContextDone(c) {
		return fmt.Errorf("request context done: %w", c.Request.Context().Err())
	}

	if _, err := c.Writer.Write([]byte(": PING\n\n")); err != nil {
		return fmt.Errorf("write ping data failed: %w", err)
	}
	return FlushWriter(c)
}

func ObjectData(c *gin.Context, object interface{}) error {
	if object == nil {
		return recordStreamOutputError(c, errors.New("object is nil"))
	}
	jsonData, err := common.Marshal(object)
	if err != nil {
		return recordStreamOutputError(c, fmt.Errorf("error marshalling object: %w", err))
	}
	return StringData(c, string(jsonData))
}

func Done(c *gin.Context) {
	_ = StringData(c, "[DONE]")
}

func WssString(c *gin.Context, ws *websocket.Conn, str string) error {
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", str))
	return ws.WriteMessage(1, []byte(str))
}

func WssObject(c *gin.Context, ws *websocket.Conn, object interface{}) error {
	jsonData, err := common.Marshal(object)
	if err != nil {
		return fmt.Errorf("error marshalling object: %w", err)
	}
	if ws == nil {
		logger.LogError(c, "websocket connection is nil")
		return errors.New("websocket connection is nil")
	}
	//common.LogInfo(c, fmt.Sprintf("sending message: %s", jsonData))
	return ws.WriteMessage(1, jsonData)
}

func WssError(c *gin.Context, ws *websocket.Conn, openaiError types.OpenAIError) {
	if ws == nil {
		return
	}
	errorObj := &dto.RealtimeEvent{
		Type:    "error",
		EventId: GetLocalRealtimeID(c),
		Error:   &openaiError,
	}
	_ = WssObject(c, ws, errorObj)
}

func GetResponseID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("chatcmpl-%s", logID)
}

func GetLocalRealtimeID(c *gin.Context) string {
	logID := c.GetString(common.RequestIdKey)
	return fmt.Sprintf("evt_%s", logID)
}

func GenerateStartEmptyResponse(id string, createAt int64, model string, systemFingerprint *string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: systemFingerprint,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Delta: dto.ChatCompletionsStreamResponseChoiceDelta{
					Role:    "assistant",
					Content: common.GetPointer(""),
				},
			},
		},
	}
}

func GenerateStopResponse(id string, createAt int64, model string, finishReason string) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				FinishReason: &finishReason,
			},
		},
	}
}

func GenerateFinalUsageResponse(id string, createAt int64, model string, usage dto.Usage) *dto.ChatCompletionsStreamResponse {
	return &dto.ChatCompletionsStreamResponse{
		Id:                id,
		Object:            "chat.completion.chunk",
		Created:           createAt,
		Model:             model,
		SystemFingerprint: nil,
		Choices:           make([]dto.ChatCompletionsStreamResponseChoice, 0),
		Usage:             &usage,
	}
}
