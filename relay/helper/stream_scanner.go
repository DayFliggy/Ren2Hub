package helper

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	InitialScannerBufferSize    = 64 << 10
	DefaultMaxScannerBufferSize = 128 << 20
	DefaultPingInterval         = 10 * time.Second
	streamWriteTimeout          = 30 * time.Second
	MaxStreamPreludeEvents      = 64
	MaxStreamPreludeBytes       = 256 << 10
)

func getScannerBufferSize() int {
	if constant.StreamScannerMaxBufferMB > 0 {
		return constant.StreamScannerMaxBufferMB << 20
	}
	return DefaultMaxScannerBufferSize
}

func NewStreamScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, InitialScannerBufferSize), getScannerBufferSize())
	return scanner
}

func copyCodexSSEHeaders(c *gin.Context, resp *http.Response) {
	if c == nil || c.Writer == nil || resp == nil {
		return
	}
	for _, name := range []string{"X-Reasoning-Included", "X-Codex-Turn-State"} {
		values := resp.Header.Values(name)
		if !service.ShouldCopyUpstreamHeader(c, name, values) {
			continue
		}
		for _, value := range values {
			if value != "" {
				c.Writer.Header().Add(name, value)
			}
		}
	}
}

func ExtendWriteDeadline(c *gin.Context) {
	if c != nil && c.Writer != nil {
		_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(streamWriteTimeout))
	}
}

// StreamPreludeWriter withholds nonsemantic frames until the attempt produces
// output. A failed attempt can then be retried without exposing its prelude.
type StreamPreludeWriter struct {
	gin.ResponseWriter
	buffer    bytes.Buffer
	committed bool
	err       error
	written   int
}

func (w *StreamPreludeWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.committed {
		n, err := w.ResponseWriter.Write(data)
		w.err = err
		w.written += n
		return n, err
	}
	if w.buffer.Len()+len(data) > MaxStreamPreludeBytes {
		w.err = fmt.Errorf("stream prelude exceeds %d bytes", MaxStreamPreludeBytes)
		return 0, w.err
	}
	n, err := w.buffer.Write(data)
	w.written += n
	return n, err
}
func (w *StreamPreludeWriter) WriteString(data string) (int, error) { return w.Write([]byte(data)) }
func (w *StreamPreludeWriter) WriteHeaderNow() {
	if w.committed {
		w.ResponseWriter.WriteHeaderNow()
	}
}
func (w *StreamPreludeWriter) Flush() {
	if w.committed && w.err == nil {
		w.ResponseWriter.Flush()
	}
}
func (w *StreamPreludeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *StreamPreludeWriter) Commit() error {
	if w.err != nil {
		return w.err
	}
	if w.committed {
		return nil
	}
	w.committed = true
	hasData := w.buffer.Len() != 0
	if hasData {
		n, err := w.ResponseWriter.Write(w.buffer.Bytes())
		w.err = err
		if w.err == nil && n != w.buffer.Len() {
			w.err = io.ErrShortWrite
		}
		w.buffer.Reset()
	}
	if w.err == nil && hasData {
		w.ResponseWriter.Flush()
	}
	return w.err
}

// StreamPayloadState distinguishes protocol terminals and semantic payload from
// headers, roles, accounting-only frames and keepalives.
func StreamPayloadState(data string) (semantic, terminal bool) {
	value := gjson.Parse(data)
	typ := value.Get("type").String()
	switch typ {
	case "image_generation.completed", "image_edit.completed":
		semantic, terminal = true, true
	case "image_generation.partial_image", "image_edit.partial_image":
		semantic = true
	case "message_stop", "response.completed", "response.done", "message_end":
		terminal = true
	case "content_block_delta":
		semantic = value.Get("delta.text").String() != "" || value.Get("delta.thinking").String() != "" || value.Get("delta.partial_json").String() != "" || value.Get("delta.signature").String() != ""
	case "content_block_start":
		semantic = value.Get("content_block.type").String() == "tool_use" || value.Get("content_block.text").String() != ""
	case "response.output_text.delta", "response.reasoning_text.delta", "response.reasoning_summary_text.delta", "response.function_call_arguments.delta", "response.custom_tool_call_input.delta":
		semantic = value.Get("delta").String() != ""
	case "response.output_item.added":
		itemType := value.Get("item.type").String()
		semantic = itemType == "function_call" || itemType == "custom_tool_call"
	}
	for _, choice := range value.Get("choices").Array() {
		if choice.Get("finish_reason").String() != "" {
			terminal = true
		}
		if choice.Get("text").String() != "" || choice.Get("delta.content").String() != "" || choice.Get("delta.reasoning_content").String() != "" || choice.Get("delta.reasoning").String() != "" || len(choice.Get("delta.tool_calls").Array()) != 0 || choice.Get("delta.function_call").Exists() {
			semantic = true
		}
	}
	for _, candidate := range value.Get("candidates").Array() {
		if candidate.Get("finishReason").String() != "" {
			terminal = true
		}
		for _, part := range candidate.Get("content.parts").Array() {
			if part.Get("text").String() != "" || part.Get("functionCall").Exists() || part.Get("inlineData").Exists() {
				semantic = true
			}
		}
	}
	return
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult)) {
	streamScannerHandler(c, resp, info, dataHandler, false)
}

// NDJSONStreamScannerHandler shares cancellation, prelude and termination
// handling with SSE while passing complete JSON lines to the provider parser.
func NDJSONStreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult)) {
	LineStreamScannerHandler(c, resp, info, dataHandler)
}

// LineStreamScannerHandler preserves provider-specific fields and text spacing.
// The parser must explicitly mark semantic output and protocol completion.
func LineStreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult)) {
	streamScannerHandler(c, resp, info, dataHandler, true)
}

func streamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult), ndjson bool) {
	info.StreamStatus = relaycommon.NewStreamStatus()
	status := info.StreamStatus
	if resp == nil || resp.Body == nil || dataHandler == nil {
		status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("invalid stream response"))
		return
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	readerDone := make(chan struct{})
	type scanItem struct {
		data string
		err  error
		eof  bool
	}
	items := make(chan scanItem)
	go func() {
		defer close(readerDone)
		defer close(items)
		scanner := NewStreamScanner(resp.Body)
		for scanner.Scan() {
			select {
			case items <- scanItem{data: scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		select {
		case items <- scanItem{eof: true, err: scanner.Err()}:
		case <-ctx.Done():
		}
	}()
	original := c.Writer
	writer := &StreamPreludeWriter{ResponseWriter: original, committed: original.Written()}
	c.Writer = writer
	defer func() {
		cancel()
		_ = resp.Body.Close()
		<-readerDone
		c.Writer = original
		if r := recover(); r != nil {
			status.SetEndReason(relaycommon.StreamEndReasonPanic, fmt.Errorf("stream handler panic: %v", r))
		}
	}()
	copyCodexSSEHeaders(c, resp)
	SetEventStreamHeaders(c)
	timeout := time.Duration(constant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	settings := operation_setting.GetGeneralSetting()
	var ping <-chan time.Time
	if settings.PingIntervalEnabled && !info.DisablePing {
		interval := time.Duration(settings.PingIntervalSeconds) * time.Second
		if interval <= 0 {
			interval = DefaultPingInterval
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		ping = ticker.C
	}
	sr := newStreamResult(status)
	terminalSeen := false
	preludeEvents := 0
	eventName := ""
	for {
		select {
		case <-ctx.Done():
			status.SetEndReason(relaycommon.StreamEndReasonClientGone, ctx.Err())
			return
		case <-timer.C:
			status.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("upstream stream timeout"))
			return
		case <-ping:
			// A heartbeat must not commit an otherwise retryable attempt.
			if writer.committed {
				ExtendWriteDeadline(c)
				if err := PingData(c); err != nil {
					status.SetEndReason(relaycommon.StreamEndReasonPingFail, err)
					return
				}
			}
		case item, ok := <-items:
			if !ok || item.eof {
				if item.err != nil {
					status.SetEndReason(relaycommon.StreamEndReasonScannerErr, item.err)
				} else if terminalSeen {
					status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				} else {
					status.SetEndReason(relaycommon.StreamEndReasonEOF, io.ErrUnexpectedEOF)
				}
				return
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(timeout)
			data := strings.TrimSpace(item.data)
			if ndjson {
				data = strings.TrimSuffix(item.data, "\r")
			}
			if !ndjson {
				if data == "" {
					eventName = ""
					continue
				}
				if strings.HasPrefix(data, "event:") {
					eventName = strings.TrimSpace(strings.TrimPrefix(data, "event:"))
					continue
				}
			}
			if !ndjson && strings.HasPrefix(data, "data:") {
				data = strings.TrimSpace(strings.TrimPrefix(data, "data:"))
			} else if !ndjson && data != "[DONE]" {
				continue
			}
			if data == "" {
				continue
			}
			if !ndjson && data == "[DONE]" {
				status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				if writer.buffer.Len() != 0 {
					if err := writer.Commit(); err != nil {
						status.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
					}
				}
				return
			}
			info.ReceivedResponseCount++
			sr.reset()
			sr.eventName = eventName
			ExtendWriteDeadline(c)
			writtenBefore := writer.written
			dataHandler(data, sr)
			if writer.err != nil {
				sr.Stop(writer.err)
			}
			if sr.HasError() || (sr.IsStopped() && !status.IsNormalEnd()) {
				if !sr.IsStopped() {
					status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("upstream stream processing failed"))
				}
				return
			}
			semantic, terminal := StreamPayloadState(data)
			// A converter may buffer a semantic upstream fragment without
			// producing a client event. It must not commit earlier metadata.
			semantic = (semantic || sr.HasValidOutput()) && writer.written > writtenBefore
			terminalSeen = terminalSeen || terminal || status.IsNormalEnd()
			if (semantic || terminal || (sr.IsStopped() && terminalSeen)) && (writer.committed || writer.buffer.Len() != 0) {
				if err := writer.Commit(); err != nil {
					status.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
					return
				}
				if semantic && original.Written() {
					info.SetFirstResponseTime()
					info.MarkValidOutput()
				}
			}
			if !writer.committed {
				preludeEvents++
				if preludeEvents > MaxStreamPreludeEvents {
					status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("stream prelude exceeds %d events", MaxStreamPreludeEvents))
					return
				}
			}
			if sr.IsStopped() {
				return
			}
		}
	}
}
