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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
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

func NewStreamScanner(reader io.Reader, maxBytes ...int) *bufio.Scanner {
	limit := getScannerBufferSize()
	if len(maxBytes) > 0 && maxBytes[0] > 0 {
		limit = min(limit, maxBytes[0])
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, min(InitialScannerBufferSize, limit)), limit)
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
	buffer        bytes.Buffer
	committed     bool
	commitOnWrite bool
	err           error
	written       int
}

func (w *StreamPreludeWriter) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if w.commitOnWrite {
		if err := w.Commit(); err != nil {
			return 0, err
		}
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
	w.commitOnWrite = false
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

// CommitStreamOutput authorizes the next semantic event write to commit the
// stream. Deferring the actual commit until Write lets state binding and output
// validation reject the event without exposing buffered metadata.
func CommitStreamOutput(c *gin.Context) error {
	if c == nil || c.Writer == nil {
		return fmt.Errorf("context or writer is nil")
	}
	if requestContextDone(c) {
		return recordStreamOutputError(c, fmt.Errorf("%w: %w", ErrStreamClientWrite, c.Request.Context().Err()))
	}
	if writer, ok := c.Writer.(*StreamPreludeWriter); ok {
		writer.commitOnWrite = true
		return writer.err
	}
	return nil
}

// StreamPayloadState distinguishes protocol terminals and semantic payload from
// headers, roles, accounting-only frames and keepalives.
func StreamPayloadState(data string) (semantic, terminal bool) {
	if !gjson.Valid(data) {
		return false, false
	}
	value := gjson.Parse(data)
	typ := value.Get("type").String()
	switch typ {
	case "image_generation.completed", "image_edit.completed":
		semantic, terminal = true, true
	case "image_generation.partial_image", "image_edit.partial_image":
		semantic = true
	case "response.image_generation_call.partial_image":
		semantic = value.Get("partial_image_b64").String() != ""
	case "response.output_item.done":
		semantic = value.Get("item.type").String() == "image_generation_call" && value.Get("item.status").String() == "completed" && value.Get("item.result").String() != ""
	case "message_stop", "message_end":
		terminal = true
	case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled":
		status := value.Get("response.status").String()
		if status == "" {
			status = strings.TrimPrefix(typ, "response.")
			if status == "done" {
				status = "completed"
			}
		}
		terminal = types.ClassifyResponseTermination(types.RelayFormatOpenAIResponses, status, value.Get("response.incomplete_details.reason").String()).IsSuccessful()
		if terminal {
			for _, output := range value.Get("response.output").Array() {
				switch output.Get("type").String() {
				case "message":
					for _, part := range output.Get("content").Array() {
						semantic = semantic || part.Get("text").String() != "" || part.Get("refusal").String() != ""
					}
				case "function_call", "custom_tool_call":
					semantic = semantic || output.Get("call_id").String() != ""
				case "image_generation_call":
					semantic = semantic || output.Get("status").String() == "completed" && output.Get("result").String() != ""
				}
			}
		}
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
	choices := value.Get("choices").Array()
	allChoicesDone := len(choices) > 0
	for _, choice := range choices {
		allChoicesDone = allChoicesDone && types.ClassifyResponseTermination(types.RelayFormatOpenAI, "", choice.Get("finish_reason").String()).IsSuccessful()
		if choice.Get("text").String() != "" || choice.Get("delta.content").String() != "" || choice.Get("delta.reasoning_content").String() != "" || choice.Get("delta.reasoning").String() != "" || len(choice.Get("delta.tool_calls").Array()) != 0 || choice.Get("delta.function_call").Exists() {
			semantic = true
		}
	}
	candidates := value.Get("candidates").Array()
	allCandidatesDone := len(candidates) > 0
	for _, candidate := range candidates {
		allCandidatesDone = allCandidatesDone && types.ClassifyResponseTermination(types.RelayFormatGemini, "", candidate.Get("finishReason").String()).IsSuccessful()
		for _, part := range candidate.Get("content.parts").Array() {
			if part.Get("text").String() != "" || part.Get("functionCall").Exists() || part.Get("inlineData").Exists() {
				semantic = true
			}
		}
	}
	terminal = terminal || allChoicesDone || allCandidatesDone
	return
}

// Candidate finish frames may arrive independently. A terminal for one index
// never completes another index, and a later index invalidates earlier EOF
// eligibility. The handler remains responsible for provider-specific errors.
type streamCandidateTerminals map[string]types.ResponseTermination

func (s streamCandidateTerminals) observe(data string) {
	if !gjson.Valid(data) {
		return
	}
	value := gjson.Parse(data)
	for _, family := range []struct {
		path, reason string
		format       types.RelayFormat
	}{
		{"choices", "finish_reason", types.RelayFormatOpenAI},
		{"candidates", "finishReason", types.RelayFormatGemini},
	} {
		for _, candidate := range value.Get(family.path).Array() {
			key := fmt.Sprintf("%s:%d", family.path, candidate.Get("index").Int())
			terminal := types.ClassifyResponseTermination(family.format, "", candidate.Get(family.reason).String())
			if _, exists := s[key]; !exists || terminal.IsTerminal() {
				s[key] = terminal
			}
		}
	}
}

func (s streamCandidateTerminals) complete() bool {
	for _, terminal := range s {
		if !terminal.IsSuccessful() {
			return false
		}
	}
	return true
}

func StreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult)) {
	streamScannerHandler(c, resp, info, dataHandler, false, configuredStreamTimeouts())
}

// NDJSONStreamScannerHandler shares cancellation, prelude and termination
// handling with SSE while passing complete JSON lines to the provider parser.
func NDJSONStreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult)) {
	LineStreamScannerHandler(c, resp, info, dataHandler)
}

// LineStreamScannerHandler preserves provider-specific fields and text spacing.
// The parser must explicitly mark semantic output and protocol completion.
func LineStreamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult)) {
	streamScannerHandler(c, resp, info, dataHandler, true, configuredStreamTimeouts())
}

type streamTimeouts struct {
	firstOutput time.Duration
	idle        time.Duration
}

func configuredStreamTimeouts() streamTimeouts {
	return streamTimeouts{
		firstOutput: common.RelayPhaseTimeout(common.StreamingFirstOutputTimeout),
		idle:        common.RelayPhaseTimeout(constant.StreamingTimeout),
	}
}

func streamScannerHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, dataHandler func(string, *StreamResult), ndjson bool, timeouts streamTimeouts) {
	info.StreamStatus = relaycommon.NewStreamStatus()
	status := info.StreamStatus
	if resp == nil || resp.Body == nil || dataHandler == nil {
		status.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("invalid stream response"))
		return
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	readerDone := make(chan struct{})
	type scanItem struct {
		data      string
		eventName string
		activity  bool
		err       error
		eof       bool
	}
	items := make(chan scanItem)
	go func() {
		defer close(readerDone)
		defer close(items)
		scanner := NewStreamScanner(resp.Body)
		decoder := sseDecoder{maxSize: getScannerBufferSize()}
		if !ndjson {
			scanner.Split(splitSSELine)
		}
		for scanner.Scan() {
			item := scanItem{data: scanner.Text()}
			if !ndjson {
				event, err := decoder.addLine(item.data)
				item = scanItem{activity: event == nil, err: err}
				if event != nil {
					item.data, item.eventName = event.data, event.name
				}
			}
			select {
			case items <- item:
			case <-ctx.Done():
				return
			}
			if item.err != nil {
				return
			}
		}
		if !ndjson && scanner.Err() == nil {
			if event := decoder.finish(); event != nil {
				select {
				case items <- scanItem{data: event.data, eventName: event.name}:
				case <-ctx.Done():
					return
				}
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
	timer := time.NewTimer(timeouts.idle)
	timer.Stop()
	defer timer.Stop()
	var idleDeadline <-chan time.Time
	firstOutputTimer := time.NewTimer(timeouts.firstOutput)
	defer firstOutputTimer.Stop()
	firstOutputDeadline := firstOutputTimer.C
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
	candidateTerminals := make(streamCandidateTerminals)
	preludeEvents := 0
	for {
		select {
		case <-ctx.Done():
			status.SetEndReason(relaycommon.StreamEndReasonClientGone, ctx.Err())
			return
		case <-idleDeadline:
			status.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("upstream stream timeout"))
			return
		case <-firstOutputDeadline:
			status.SetEndReason(relaycommon.StreamEndReasonTimeout, fmt.Errorf("upstream first output timeout"))
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
			if ctx.Err() != nil {
				status.SetEndReason(relaycommon.StreamEndReasonClientGone, ctx.Err())
				return
			}
			if item.err != nil {
				status.SetEndReason(relaycommon.StreamEndReasonScannerErr, item.err)
				return
			}
			if !ok || item.eof {
				if terminalSeen && candidateTerminals.complete() {
					status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				} else {
					status.SetEndReason(relaycommon.StreamEndReasonEOF, io.ErrUnexpectedEOF)
				}
				return
			}
			if idleDeadline != nil {
				timer.Reset(timeouts.idle)
			}
			if item.activity {
				continue
			}
			data := strings.TrimSuffix(item.data, "\r")
			if data == "" {
				continue
			}
			if !ndjson && data == "[DONE]" {
				if !candidateTerminals.complete() {
					status.SetEndReason(relaycommon.StreamEndReasonEOF, fmt.Errorf("%w: upstream candidate is missing a successful finish reason", io.ErrUnexpectedEOF))
					return
				}
				status.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				if writer.buffer.Len() != 0 {
					if err := writer.Commit(); err != nil {
						status.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
					}
				}
				return
			}
			info.ReceivedResponseCount++
			candidateTerminals.observe(data)
			sr.reset()
			sr.eventName = item.eventName
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
			if len(candidateTerminals) > 0 {
				terminalSeen = candidateTerminals.complete()
			}
			if sr.IsStopped() && status.IsNormalEnd() && !candidateTerminals.complete() {
				status.SetEndReason(relaycommon.StreamEndReasonEOF, fmt.Errorf("%w: upstream candidate is missing a successful finish reason", io.ErrUnexpectedEOF))
				return
			}
			if (semantic || terminal || (sr.IsStopped() && terminalSeen)) && (writer.committed || writer.buffer.Len() != 0) {
				if err := writer.Commit(); err != nil {
					status.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
					return
				}
				if semantic && original.Written() {
					info.SetFirstResponseTime()
					info.MarkValidOutput()
					firstOutputTimer.Stop()
					firstOutputDeadline = nil
					timer.Reset(timeouts.idle)
					idleDeadline = timer.C
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
