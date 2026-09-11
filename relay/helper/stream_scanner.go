package helper

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

const (
	InitialScannerBufferSize    = 64 << 10
	DefaultMaxScannerBufferSize = 128 << 20
	DefaultPingInterval         = 10 * time.Second
	streamWriteTimeout          = 30 * time.Second
	streamTotalTimeout          = 30 * time.Minute
	maxPrecommitEvents          = 1024
	maxPrecommitBytes           = 8 << 20
)

type StreamCommitLimits struct {
	MaxPrecommitEvents int `json:"max_precommit_events"`
	MaxPrecommitBytes  int `json:"max_precommit_bytes"`
}

func CurrentStreamCommitLimits() StreamCommitLimits {
	return StreamCommitLimits{
		MaxPrecommitEvents: maxPrecommitEvents,
		MaxPrecommitBytes:  maxPrecommitBytes,
	}
}

var errPrecommitBufferExceeded = errors.New("stream precommit buffer exceeded")

type streamTimeoutPolicy struct {
	FirstEvent   time.Duration
	FirstContent time.Duration
	Idle         time.Duration
	Total        time.Duration
}

var streamTimeoutPolicyProvider = defaultStreamTimeoutPolicy

func defaultStreamTimeoutPolicy() streamTimeoutPolicy {
	phaseTimeout := time.Duration(constant.StreamingTimeout) * time.Second
	if phaseTimeout <= 0 {
		phaseTimeout = 5 * time.Minute
	}
	totalTimeout := streamTotalTimeout
	if minimumTotal := 4 * phaseTimeout; minimumTotal > totalTimeout {
		totalTimeout = minimumTotal
	}
	return streamTimeoutPolicy{
		FirstEvent:   phaseTimeout,
		FirstContent: phaseTimeout,
		Idle:         phaseTimeout,
		Total:        totalTimeout,
	}
}

type streamUpdate struct {
	data   string
	reason relaycommon.StreamEndReason
	err    error
}

type precommitResponseWriter struct {
	underlying gin.ResponseWriter
	header     http.Header
	body       bytes.Buffer
	status     int
	size       int
	written    bool
	committed  bool
	bufferErr  error
}

func newPrecommitResponseWriter(underlying gin.ResponseWriter) *precommitResponseWriter {
	return &precommitResponseWriter{
		underlying: underlying,
		header:     underlying.Header().Clone(),
		status:     underlying.Status(),
		size:       -1,
	}
}

func (w *precommitResponseWriter) Header() http.Header {
	if w.committed {
		return w.underlying.Header()
	}
	return w.header
}

func (w *precommitResponseWriter) WriteHeader(code int) {
	if w.committed {
		w.underlying.WriteHeader(code)
		return
	}
	if code > 0 && !w.written {
		w.status = code
	}
}

func (w *precommitResponseWriter) WriteHeaderNow() {
	if w.committed {
		w.underlying.WriteHeaderNow()
		return
	}
	w.written = true
	if w.size < 0 {
		w.size = 0
	}
}

func (w *precommitResponseWriter) Write(data []byte) (int, error) {
	if w.committed {
		return w.underlying.Write(data)
	}
	w.WriteHeaderNow()
	if w.body.Len()+len(data) > maxPrecommitBytes {
		w.bufferErr = fmt.Errorf("%w: output exceeds %d bytes", errPrecommitBufferExceeded, maxPrecommitBytes)
		return 0, w.bufferErr
	}
	n, err := w.body.Write(data)
	w.size += n
	return n, err
}

func (w *precommitResponseWriter) WriteString(data string) (int, error) {
	return w.Write([]byte(data))
}

func (w *precommitResponseWriter) Status() int {
	if w.committed {
		return w.underlying.Status()
	}
	return w.status
}

func (w *precommitResponseWriter) Size() int {
	if w.committed {
		return w.underlying.Size()
	}
	return w.size
}

func (w *precommitResponseWriter) Written() bool {
	if w.committed {
		return w.underlying.Written()
	}
	return false
}

func (w *precommitResponseWriter) Flush() {
	if w.committed {
		w.underlying.Flush()
		return
	}
	w.WriteHeaderNow()
}

func (w *precommitResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.underlying.Hijack()
}

func (w *precommitResponseWriter) CloseNotify() <-chan bool {
	return w.underlying.CloseNotify()
}

func (w *precommitResponseWriter) Pusher() http.Pusher {
	return w.underlying.Pusher()
}

func (w *precommitResponseWriter) Unwrap() http.ResponseWriter {
	return w.underlying
}

func (w *precommitResponseWriter) bufferedBytes() int {
	return w.body.Len()
}

var eventStreamHeaderValues = map[string]string{
	"Content-Type":      "text/event-stream",
	"Cache-Control":     "no-cache",
	"Connection":        "keep-alive",
	"Transfer-Encoding": "chunked",
	"X-Accel-Buffering": "no",
}

func detachEventStreamHeaders(header http.Header) {
	for name, value := range eventStreamHeaderValues {
		if strings.EqualFold(strings.TrimSpace(header.Get(name)), value) {
			header.Del(name)
		}
	}
}

func stageEventStreamHeaders(c *gin.Context) {
	for name, value := range eventStreamHeaderValues {
		c.Writer.Header().Set(name, value)
	}
	c.Set("event_stream_headers_set", true)
}

func (w *precommitResponseWriter) commit() error {
	if w.committed {
		return nil
	}
	if w.bufferErr != nil {
		return w.bufferErr
	}

	destinationHeader := w.underlying.Header()
	for key := range destinationHeader {
		destinationHeader.Del(key)
	}
	for key, values := range w.header {
		for _, value := range values {
			destinationHeader.Add(key, value)
		}
	}

	w.committed = true
	if w.status > 0 {
		w.underlying.WriteHeader(w.status)
	}
	w.underlying.WriteHeaderNow()
	if w.body.Len() > 0 {
		if _, err := w.underlying.Write(w.body.Bytes()); err != nil {
			return err
		}
	}
	w.underlying.Flush()
	return nil
}

func (w *precommitResponseWriter) commitTerminalError() error {
	w.body.Reset()
	w.bufferErr = nil
	w.written = false
	w.size = -1
	w.status = http.StatusBadGateway
	w.header.Set("Content-Type", "text/event-stream")
	if _, err := w.WriteString("data: {\"error\":{\"message\":\"stream precommit buffer exceeded\",\"type\":\"stream_buffer_overflow\",\"code\":\"stream_buffer_overflow\"}}\n\n"); err != nil {
		return err
	}
	return w.commit()
}

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
	if c == nil || c.Writer == nil {
		return
	}
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(streamWriteTimeout))
}

func newStreamTimer(duration time.Duration) *time.Timer {
	if duration <= 0 {
		return nil
	}
	return time.NewTimer(duration)
}

func streamTimerChannel(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}

func stopStreamTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func resetStreamTimer(timer *time.Timer, duration time.Duration) {
	if timer == nil || duration <= 0 {
		return
	}
	stopStreamTimer(timer)
	timer.Reset(duration)
}

func normalizedStreamKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, ".", "")
	return key
}

func streamValueHasContent(key string, value any) bool {
	normalizedKey := normalizedStreamKey(key)
	switch typed := value.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return false
		}
		switch normalizedKey {
		case "content", "text", "answer", "result", "completion", "delta", "outputtext", "reasoning", "reasoningcontent", "thinking", "arguments", "partialjson", "transcript", "b64json", "partialimageb64", "url", "imageurl":
			return true
		}
		return false
	case []any:
		if len(typed) == 0 {
			return false
		}
		if normalizedKey == "toolcalls" || normalizedKey == "functioncalls" {
			return true
		}
		for _, item := range typed {
			if streamValueHasContent(key, item) {
				return true
			}
		}
	case map[string]any:
		if (normalizedKey == "audio" || normalizedKey == "image" || normalizedKey == "inlinedata") && len(typed) > 0 {
			return true
		}
		if typeValue, ok := typed["type"].(string); ok {
			normalizedType := normalizedStreamKey(typeValue)
			if strings.Contains(normalizedType, "tooluse") || strings.Contains(normalizedType, "functioncall") {
				return true
			}
		}
		for childKey, childValue := range typed {
			switch normalizedStreamKey(childKey) {
			case "content", "text", "answer", "result", "completion", "delta", "outputtext", "reasoning", "reasoningcontent", "thinking", "arguments", "partialjson", "transcript", "b64json", "partialimageb64", "url", "imageurl", "toolcalls", "functioncalls", "functioncall", "choices", "candidates", "parts", "output", "item", "message", "contentblock", "response", "audio", "image", "inlinedata", "data":
			default:
				continue
			}
			if streamValueHasContent(childKey, childValue) {
				return true
			}
		}
	}
	return false
}

func isMeaningfulStreamPayload(data string) bool {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" || strings.HasPrefix(trimmed, "[DONE]") {
		return false
	}
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return true
	}
	var payload any
	if err := common.Unmarshal([]byte(trimmed), &payload); err != nil {
		return false
	}
	return streamValueHasContent("", payload)
}

// IsMeaningfulStreamPayload lets adapters align delayed or converted writes
// with the scanner's shared commit boundary.
func IsMeaningfulStreamPayload(data string) bool {
	return isMeaningfulStreamPayload(data)
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(data string, sr *StreamResult)) {
	if c == nil || c.Writer == nil || c.Request == nil || resp == nil || resp.Body == nil || info == nil || dataHandler == nil {
		return
	}

	info.StreamStatus = relaycommon.NewStreamStatus()
	c.Set(streamOutputBlockedContextKey, false)
	defer func() {
		if info.StreamStatus.IsNormalEnd() && !info.StreamStatus.HasErrors() {
			logger.LogInfo(c, fmt.Sprintf("stream ended: %s", info.StreamStatus.Summary()))
			return
		}
		logger.LogError(c, fmt.Sprintf("stream ended: %s, received=%d", info.StreamStatus.Summary(), info.ReceivedResponseCount))
	}()
	ctx, cancel := context.WithCancel(context.Background())
	originalWriter := c.Writer
	detachEventStreamHeaders(originalWriter.Header())
	stagedWriter := newPrecommitResponseWriter(originalWriter)
	c.Writer = stagedWriter

	updates := make(chan streamUpdate, 1)
	scannerExited := make(chan struct{})
	scanner := NewStreamScanner(resp.Body)
	scanner.Split(bufio.ScanLines)

	defer func() {
		cancel()
		_ = resp.Body.Close()
		<-scannerExited
		c.Writer = originalWriter
	}()

	copyCodexSSEHeaders(c, resp)
	stageEventStreamHeaders(c)

	policy := streamTimeoutPolicyProvider()
	firstEventTimer := newStreamTimer(policy.FirstEvent)
	firstContentTimer := (*time.Timer)(nil)
	idleTimer := (*time.Timer)(nil)
	totalTimer := newStreamTimer(policy.Total)
	defer stopStreamTimer(firstEventTimer)
	defer stopStreamTimer(totalTimer)

	generalSettings := operation_setting.GetGeneralSetting()
	pingEnabled := generalSettings.PingIntervalEnabled && !info.DisablePing
	pingInterval := time.Duration(generalSettings.PingIntervalSeconds) * time.Second
	if pingInterval <= 0 {
		pingInterval = DefaultPingInterval
	}
	var pingTicker *time.Ticker
	var pingChannel <-chan time.Time
	defer func() {
		if pingTicker != nil {
			pingTicker.Stop()
		}
	}()

	logger.LogDebug(c, "stream timeouts: first_event=%s first_content=%s idle=%s total=%s", policy.FirstEvent, policy.FirstContent, policy.Idle, policy.Total)

	go func() {
		defer close(scannerExited)
		for scanner.Scan() {
			line := scanner.Text()
			logger.LogDebug(c, "stream scanner data: %s", line)
			if strings.HasPrefix(line, "[DONE]") {
				select {
				case updates <- streamUpdate{reason: relaycommon.StreamEndReasonDone}:
				case <-ctx.Done():
				}
				return
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(line[5:])
			if data == "" {
				continue
			}
			if strings.HasPrefix(data, "[DONE]") {
				select {
				case updates <- streamUpdate{reason: relaycommon.StreamEndReasonDone}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case updates <- streamUpdate{data: data}:
			case <-ctx.Done():
				return
			}
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			select {
			case updates <- streamUpdate{reason: relaycommon.StreamEndReasonScannerErr, err: err}:
			case <-ctx.Done():
			}
			return
		}
		select {
		case updates <- streamUpdate{reason: relaycommon.StreamEndReasonEOF}:
		case <-ctx.Done():
		}
	}()

	precommitEvents := 0
	precommitBytes := 0
	seenEvent := false
	committed := false
	finishWithError := func(reason relaycommon.StreamEndReason, err error) {
		if err != nil {
			info.StreamStatus.RecordError(err.Error())
		}
		info.StreamStatus.SetEndReason(reason, err)
	}
	blockTrailingWrites := func() {
		c.Set(streamOutputBlockedContextKey, true)
	}

	for {
		select {
		case update := <-updates:
			if update.data == "" {
				if update.err != nil {
					finishWithError(update.reason, update.err)
				} else if !committed && info.StreamStatus.HasErrors() {
					info.StreamStatus.SetEndReason(update.reason, nil)
				} else if !committed {
					finishWithError(update.reason, errors.New("upstream stream ended before meaningful content"))
				} else {
					info.StreamStatus.SetEndReason(update.reason, nil)
				}
				if !committed || info.StreamStatus.HasErrors() || !info.StreamStatus.IsNormalEnd() {
					blockTrailingWrites()
				}
				return
			}

			if !seenEvent {
				seenEvent = true
				stopStreamTimer(firstEventTimer)
				firstContentTimer = newStreamTimer(policy.FirstContent)
				defer stopStreamTimer(firstContentTimer)
				idleTimer = newStreamTimer(policy.Idle)
				defer stopStreamTimer(idleTimer)
			}
			resetStreamTimer(idleTimer, policy.Idle)
			info.SetFirstResponseTime()
			info.ReceivedResponseCount++

			sr := newStreamResult(info.StreamStatus)
			var panicErr error
			func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						panicErr = fmt.Errorf("stream handler panic: %v", recovered)
					}
				}()
				ExtendWriteDeadline(c)
				dataHandler(update.data, sr)
			}()
			if panicErr != nil {
				finishWithError(relaycommon.StreamEndReasonPanic, panicErr)
				blockTrailingWrites()
				return
			}

			meaningful := sr.HasValidOutput() || isMeaningfulStreamPayload(update.data)
			stoppedNormally := sr.IsStopped() && info.StreamStatus.EndReason == relaycommon.StreamEndReasonDone
			if !committed && meaningful && !sr.HasError() && (!sr.IsStopped() || stoppedNormally) {
				info.MarkValidOutput()
				if err := stagedWriter.commit(); err != nil {
					finishWithError(relaycommon.StreamEndReasonClientGone, err)
					blockTrailingWrites()
					return
				}
				committed = true
				stopStreamTimer(firstContentTimer)
				if pingEnabled {
					pingTicker = time.NewTicker(pingInterval)
					pingChannel = pingTicker.C
				}
			}

			if !committed {
				precommitEvents++
				precommitBytes += len(update.data)
				if precommitEvents > maxPrecommitEvents || precommitBytes > maxPrecommitBytes || stagedWriter.bufferedBytes() > maxPrecommitBytes || errors.Is(stagedWriter.bufferErr, errPrecommitBufferExceeded) {
					overflowErr := fmt.Errorf("%w: events=%d bytes=%d output_bytes=%d", errPrecommitBufferExceeded, precommitEvents, precommitBytes, stagedWriter.bufferedBytes())
					finishWithError(relaycommon.StreamEndReasonHandlerStop, overflowErr)
					info.MarkValidOutput()
					if err := stagedWriter.commitTerminalError(); err != nil {
						info.StreamStatus.RecordError(err.Error())
					}
					blockTrailingWrites()
					return
				}
			}

			if sr.IsStopped() {
				if !committed {
					if !info.StreamStatus.HasErrors() {
						info.StreamStatus.RecordError("upstream stream ended before meaningful content")
					}
					blockTrailingWrites()
				} else if info.StreamStatus.HasErrors() || !info.StreamStatus.IsNormalEnd() {
					blockTrailingWrites()
				}
				return
			}

		case <-streamTimerChannel(firstEventTimer):
			finishWithError(relaycommon.StreamEndReasonTimeout, errors.New("stream first_event timeout"))
			blockTrailingWrites()
			return
		case <-streamTimerChannel(firstContentTimer):
			finishWithError(relaycommon.StreamEndReasonTimeout, errors.New("stream first_content timeout"))
			blockTrailingWrites()
			return
		case <-streamTimerChannel(idleTimer):
			finishWithError(relaycommon.StreamEndReasonTimeout, errors.New("stream idle timeout"))
			blockTrailingWrites()
			return
		case <-streamTimerChannel(totalTimer):
			finishWithError(relaycommon.StreamEndReasonTimeout, errors.New("stream total timeout"))
			blockTrailingWrites()
			return
		case <-pingChannel:
			ExtendWriteDeadline(c)
			if err := PingData(c); err != nil {
				finishWithError(relaycommon.StreamEndReasonPingFail, err)
				blockTrailingWrites()
				return
			}
		case <-c.Request.Context().Done():
			finishWithError(relaycommon.StreamEndReasonClientGone, c.Request.Context().Err())
			blockTrailingWrites()
			return
		}
	}
}
