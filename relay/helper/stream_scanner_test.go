package helper

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
	if constant.StreamingTimeout == 0 {
		constant.StreamingTimeout = 30
	}
}

func setupStreamTest(t *testing.T, body io.Reader) (*gin.Context, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		Body: io.NopCloser(body),
	}

	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	return c, resp, info
}

func buildSSEBody(n int) string {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "data: {\"id\":%d,\"choices\":[{\"delta\":{\"content\":\"token_%d\"}}]}\n", i, i)
	}
	b.WriteString("data: [DONE]\n")
	return b.String()
}

type failingStreamResponseWriter struct {
	gin.ResponseWriter
	err error
}

func (w *failingStreamResponseWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

type closeTrackingReader struct {
	io.Reader
	closed atomic.Bool
}

func (r *closeTrackingReader) Close() error {
	r.closed.Store(true)
	return nil
}

// ---------- Basic correctness ----------

func TestStreamScannerHandler_NilInputs(t *testing.T) {
	t.Parallel()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, nil, info, func(data string, sr *StreamResult) {})
	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(""))}, info, nil)
}

func TestNewStreamScanner_AllowsLargeStreamLine(t *testing.T) {
	oldBufferMB := constant.StreamScannerMaxBufferMB
	constant.StreamScannerMaxBufferMB = 1
	t.Cleanup(func() {
		constant.StreamScannerMaxBufferMB = oldBufferMB
	})

	payload := strings.Repeat("x", 128<<10)
	scanner := NewStreamScanner(strings.NewReader("data: " + payload + "\n"))
	scanner.Split(bufio.ScanLines)

	require.True(t, scanner.Scan())
	assert.Equal(t, "data: "+payload, scanner.Text())
	require.NoError(t, scanner.Err())
}

func TestStreamScannerHandler_EmptyBody(t *testing.T) {
	t.Parallel()

	c, resp, info := setupStreamTest(t, strings.NewReader(""))

	var called atomic.Bool
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		called.Store(true)
	})

	assert.False(t, called.Load(), "handler should not be called for empty body")
}

func TestStreamScannerHandler_1000Chunks(t *testing.T) {
	t.Parallel()

	const numChunks = 1000
	body := buildSSEBody(numChunks)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
	})

	assert.Equal(t, int64(numChunks), count.Load())
	assert.Equal(t, numChunks, info.ReceivedResponseCount)
}

func TestStreamScannerHandler_OrderPreserved(t *testing.T) {
	t.Parallel()

	const numChunks = 500
	body := buildSSEBody(numChunks)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var mu sync.Mutex
	received := make([]string, 0, numChunks)

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		mu.Lock()
		received = append(received, data)
		mu.Unlock()
	})

	require.Equal(t, numChunks, len(received))
	for i := 0; i < numChunks; i++ {
		expected := fmt.Sprintf("{\"id\":%d,\"choices\":[{\"delta\":{\"content\":\"token_%d\"}}]}", i, i)
		assert.Equal(t, expected, received[i], "chunk %d out of order", i)
	}
}

func TestStreamScannerHandler_DoneStopsScanner(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(50) + "data: should_not_appear\n"
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
	})

	assert.Equal(t, int64(50), count.Load(), "data after [DONE] must not be processed")
}

func TestStreamScannerHandler_StopStopsStream(t *testing.T) {
	t.Parallel()

	const numChunks = 200
	body := buildSSEBody(numChunks)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	const stopAt int64 = 50
	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		n := count.Add(1)
		if n >= stopAt {
			sr.Stop(fmt.Errorf("fatal at %d", n))
		}
	})

	assert.Equal(t, stopAt, count.Load())
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
}

func TestStreamScannerHandler_SkipsNonDataLines(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString(": comment line\n")
	b.WriteString("event: message\n")
	b.WriteString("id: 12345\n")
	b.WriteString("retry: 5000\n")
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "data: payload_%d\n", i)
		b.WriteString(": interleaved comment\n")
	}
	b.WriteString("data: [DONE]\n")

	c, resp, info := setupStreamTest(t, strings.NewReader(b.String()))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
	})

	assert.Equal(t, int64(100), count.Load())
}

func TestStreamScannerHandler_DataWithExtraSpaces(t *testing.T) {
	t.Parallel()

	body := "data:   {\"trimmed\":true}  \ndata: [DONE]\n"
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var got string
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		got = data
	})

	assert.Equal(t, "{\"trimmed\":true}", got)
}

// TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns pins the
// disconnect contract: when the client goes away, the handler must return
// promptly (all goroutines joined, so the gin.Context can never leak into a
// pooled reuse), the upstream body must be closed to stop token generation,
// and no data received after the disconnect may be processed or written.
func TestStreamScannerHandler_ClientCancelAbortsUpstreamAndReturns(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)

	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	var count atomic.Int64
	firstHandled := make(chan struct{})
	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			count.Add(1)
			_ = StringData(c, data)
			if data == "first" {
				close(firstHandled)
			}
		})
		close(done)
	}()

	_, err := fmt.Fprint(pw, "data: first\n")
	require.NoError(t, err)

	select {
	case <-firstHandled:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first chunk")
	}

	cancel()

	// The handler must return without any further upstream input: cleanup
	// closes resp.Body, which unblocks the scanner goroutine.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after client disconnect")
	}

	// Upstream read side must be closed so the provider stops generating
	// (and billing) for a request nobody is listening to.
	_, err = fmt.Fprint(pw, "data: second\n")
	require.ErrorIs(t, err, io.ErrClosedPipe, "upstream body should be closed after client disconnect")

	assert.Equal(t, int64(1), count.Load(), "no chunk after disconnect should be processed")
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)

	body := recorder.Body.String()
	assert.Contains(t, body, "first")
	assert.NotContains(t, body, "second")
}

// ---------- Ping tests ----------

func TestStreamScannerHandler_PingSentDuringSlowUpstream(t *testing.T) {
	setting := operation_setting.GetGeneralSetting()
	oldEnabled := setting.PingIntervalEnabled
	oldSeconds := setting.PingIntervalSeconds
	setting.PingIntervalEnabled = true
	setting.PingIntervalSeconds = 1
	t.Cleanup(func() {
		setting.PingIntervalEnabled = oldEnabled
		setting.PingIntervalSeconds = oldSeconds
	})

	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		for i := 0; i < 4; i++ {
			fmt.Fprintf(pw, "data: chunk_%d\n", i)
			time.Sleep(400 * time.Millisecond)
		}
		fmt.Fprint(pw, "data: [DONE]\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	var count atomic.Int64
	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			count.Add(1)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream to finish")
	}

	assert.Equal(t, int64(4), count.Load())

	body := recorder.Body.String()
	pingCount := strings.Count(body, ": PING")
	assert.GreaterOrEqual(t, pingCount, 1,
		"expected at least 1 ping during slow stream with 1s interval; got %d", pingCount)
}

func TestStreamScannerHandler_PingDisabledByRelayInfo(t *testing.T) {
	setting := operation_setting.GetGeneralSetting()
	oldEnabled := setting.PingIntervalEnabled
	oldSeconds := setting.PingIntervalSeconds
	setting.PingIntervalEnabled = true
	setting.PingIntervalSeconds = 1
	t.Cleanup(func() {
		setting.PingIntervalEnabled = oldEnabled
		setting.PingIntervalSeconds = oldSeconds
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{Body: io.NopCloser(strings.NewReader(buildSSEBody(5)))}
	info := &relaycommon.RelayInfo{
		DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{},
	}

	var count atomic.Int64
	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
			count.Add(1)
		})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	assert.Equal(t, int64(5), count.Load())

	body := recorder.Body.String()
	pingCount := strings.Count(body, ": PING")
	assert.Equal(t, 0, pingCount, "pings should be disabled when DisablePing=true")
}

// ---------- StreamStatus integration ----------

func TestStreamScannerHandler_StreamStatus_DoneReason(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(10)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.Nil(t, info.StreamStatus.EndError)
	assert.True(t, info.StreamStatus.IsNormalEnd())
	assert.False(t, info.StreamStatus.HasErrors())
}

func TestStreamScannerHandler_StreamStatus_EOFWithoutDone(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	for i := 0; i < 5; i++ {
		fmt.Fprintf(&b, "data: {\"id\":%d,\"choices\":[{\"delta\":{\"content\":\"token_%d\"}}]}\n", i, i)
	}
	c, resp, info := setupStreamTest(t, strings.NewReader(b.String()))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonEOF, info.StreamStatus.EndReason)
	assert.True(t, info.StreamStatus.IsNormalEnd())
}

func TestStreamScannerHandler_StreamStatus_HandlerStop(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(100)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		n := count.Add(1)
		if n >= 10 {
			sr.Stop(fmt.Errorf("stop at 10"))
		}
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.True(t, info.StreamStatus.HasErrors())
}

func TestStreamScannerHandler_StreamStatus_HandlerDone(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(20)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		n := count.Add(1)
		if n >= 5 {
			sr.Done()
		}
	})

	assert.Equal(t, int64(5), count.Load())
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.False(t, info.StreamStatus.HasErrors())
}

func TestStreamScannerHandler_StreamStatus_Timeout(t *testing.T) {
	// Not parallel: modifies global constant.StreamingTimeout
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	pr, pw := io.Pipe()
	go func() {
		fmt.Fprint(pw, "data: {\"id\":1}\n")
		time.Sleep(2 * time.Second)
		pw.Close()
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{Body: pr}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	done := make(chan struct{})
	go func() {
		StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream timeout")
	}

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	assert.False(t, info.StreamStatus.IsNormalEnd())
}

func TestStreamScannerHandler_StreamStatus_SoftErrors(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(10)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		sr.Error(fmt.Errorf("soft error for chunk"))
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.True(t, info.StreamStatus.HasErrors())
	assert.Equal(t, 10, info.StreamStatus.TotalErrorCount())
}

func TestStreamScannerHandler_StreamStatus_MultipleErrorsPerChunk(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(5)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		sr.Error(fmt.Errorf("error A"))
		sr.Error(fmt.Errorf("error B"))
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.Equal(t, 10, info.StreamStatus.TotalErrorCount())
}

func TestStreamScannerHandler_StreamStatus_ErrorThenStop(t *testing.T) {
	t.Parallel()

	// Use a large body without [DONE] to avoid race between scanner's [DONE]
	// and handler's Stop on the sync.Once EndReason.
	var b strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&b, "data: {\"id\":%d}\n", i)
	}
	c, resp, info := setupStreamTest(t, strings.NewReader(b.String()))

	var count atomic.Int64
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		count.Add(1)
		sr.Error(fmt.Errorf("soft error"))
		sr.Stop(fmt.Errorf("fatal"))
	})

	assert.Equal(t, int64(1), count.Load())
	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.Equal(t, 2, info.StreamStatus.TotalErrorCount())
}

func TestStreamScannerHandler_StreamStatus_InitializedIfNil(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(1)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	assert.Nil(t, info.StreamStatus)

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	assert.NotNil(t, info.StreamStatus)
}

func TestStreamScannerHandler_StreamStatus_ReplacesPreInitialized(t *testing.T) {
	t.Parallel()

	body := buildSSEBody(5)
	c, resp, info := setupStreamTest(t, strings.NewReader(body))

	info.StreamStatus = relaycommon.NewStreamStatus()
	info.StreamStatus.RecordError("pre-existing error")

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {})

	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.Equal(t, 0, info.StreamStatus.TotalErrorCount())
}

func TestIsMeaningfulStreamPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want bool
	}{
		{name: "response metadata", data: `{"type":"response.created","response":{"id":"resp_1","model":"gpt-5"}}`, want: false},
		{name: "metadata text does not commit", data: `{"type":"response.created","metadata":{"text":"tag","url":"https://example.com"}}`, want: false},
		{name: "assistant role", data: `{"choices":[{"delta":{"role":"assistant"}}]}`, want: false},
		{name: "text delta", data: `{"choices":[{"delta":{"content":"hello"}}]}`, want: true},
		{name: "tool call", data: `{"choices":[{"delta":{"tool_calls":[{"id":"call_1","type":"function"}]}}]}`, want: true},
		{name: "finish reason", data: `{"choices":[{"delta":{},"finish_reason":"stop"}]}`, want: false},
		{name: "responses completed", data: `{"type":"response.completed","response":{"status":"completed"}}`, want: false},
		{name: "claude message stop", data: `{"type":"message_stop"}`, want: false},
		{name: "partial image", data: `{"type":"image_generation.partial_image","partial_image_b64":"image-bytes"}`, want: true},
		{name: "dify message end", data: `{"event":"message_end","conversation_id":"conv_1"}`, want: false},
		{name: "plain payload", data: "hello", want: true},
		{name: "invalid json", data: "{", want: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, isMeaningfulStreamPayload(test.data))
		})
	}
}

func TestStreamScannerHandler_MetadataFailureDoesNotCommit(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetEventStreamHeaders(c)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n" +
			"data: invalid-json\n",
	))}
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}
	stagedReportedWritten := false

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
		stagedReportedWritten = c.Writer.Written()
		if data == "invalid-json" {
			sr.Stop(errors.New("invalid upstream event"))
		}
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.False(t, info.HasValidOutput())
	assert.False(t, stagedReportedWritten)
	assert.False(t, c.Writer.Written())
	assert.Empty(t, recorder.Header())
	assert.Empty(t, recorder.Body.String())

	c.JSON(http.StatusBadGateway, gin.H{"error": "upstream failed"})
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"error":"upstream failed"}`, recorder.Body.String())
}

func TestStreamScannerHandler_BlocksTrailingDoneAfterFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader("data: invalid-json\n"))}, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
		sr.Stop(errors.New("invalid upstream event"))
	})
	Done(c)

	assert.False(t, info.HasValidOutput())
	assert.Empty(t, recorder.Body.String())
	assert.False(t, c.Writer.Written())
}

func TestStreamScannerHandler_BlocksAllTrailingSSEWritesAfterFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader("data: invalid-json\n"))}, info, func(data string, sr *StreamResult) {
		sr.Stop(errors.New("invalid upstream event"))
	})

	assert.Error(t, ClaudeData(c, dto.ClaudeResponse{Type: "message_stop"}))
	ClaudeChunkData(c, dto.ClaudeResponse{Type: "message_stop"}, `{"type":"message_stop"}`)
	assert.Error(t, ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.completed"}, `{"type":"response.completed"}`))
	assert.Error(t, PingData(c))
	assert.Empty(t, recorder.Body.String())
	assert.False(t, c.Writer.Written())
}

func TestResetStreamAttemptStateAllowsFreshStreamWrites(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader("data: invalid-json\n"))}, info, func(data string, sr *StreamResult) {
		sr.Stop(errors.New("invalid upstream event"))
	})
	require.Error(t, StringData(c, `{"choices":[{"delta":{"content":"blocked"}}]}`))

	ResetStreamAttemptState(c)
	require.NoError(t, StringData(c, `{"choices":[{"delta":{"content":"retry"}}]}`))
	assert.Contains(t, recorder.Body.String(), "retry")
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
}

func TestStreamScannerHandler_WriteFailureStopsAndClosesUpstream(t *testing.T) {
	writeErr := errors.New("client write failed")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Writer = &failingStreamResponseWriter{ResponseWriter: c.Writer, err: writeErr}
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	body := &closeTrackingReader{Reader: strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\ndata: {\"choices\":[{\"delta\":{\"content\":\"later\"}}]}\n")}
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: body}, info, func(data string, sr *StreamResult) {
		if err := StringData(c, data); err != nil {
			sr.Stop(err)
		}
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	require.Error(t, info.StreamStatus.EndError)
	assert.ErrorIs(t, info.StreamStatus.EndError, writeErr)
	assert.True(t, body.closed.Load())
	assert.Empty(t, recorder.Body.String())
}

func TestStreamScannerHandler_DoneWithoutContentIsFailure(t *testing.T) {
	c, resp, info := setupStreamTest(t, strings.NewReader("data: {\"event\":\"message_end\"}\n"))

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		sr.Done()
	})

	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.StreamStatus.HasErrors())
	assert.False(t, info.HasValidOutput())
}

func TestStreamScannerHandler_TerminalMetadataDoesNotCommit(t *testing.T) {
	tests := []string{
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
		`{"type":"message_stop"}`,
	}
	for _, terminal := range tests {
		terminal := terminal
		t.Run(terminal, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{Body: io.NopCloser(strings.NewReader("data: " + terminal + "\ndata: [DONE]\n"))}
			info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

			StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
				require.NoError(t, StringData(c, data))
			})

			assert.False(t, info.HasValidOutput())
			assert.True(t, info.StreamStatus.HasErrors())
			assert.False(t, c.Writer.Written())
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestStreamScannerHandler_ContentCommitsBufferedMetadata(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n" +
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n" +
			"data: [DONE]\n",
	))}
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
	})

	body := recorder.Body.String()
	assert.Contains(t, body, "response.created")
	assert.Contains(t, body, "hello")
	assert.True(t, info.HasValidOutput())
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
}

func TestStreamScannerHandler_DoesNotPingBeforeContent(t *testing.T) {
	setting := operation_setting.GetGeneralSetting()
	oldEnabled := setting.PingIntervalEnabled
	oldSeconds := setting.PingIntervalSeconds
	setting.PingIntervalEnabled = true
	setting.PingIntervalSeconds = 1
	t.Cleanup(func() {
		setting.PingIntervalEnabled = oldEnabled
		setting.PingIntervalSeconds = oldSeconds
	})

	oldPolicyProvider := streamTimeoutPolicyProvider
	streamTimeoutPolicyProvider = func() streamTimeoutPolicy {
		return streamTimeoutPolicy{
			FirstEvent:   3 * time.Second,
			FirstContent: 2 * time.Second,
			Idle:         3 * time.Second,
			Total:        3 * time.Second,
		}
	}
	t.Cleanup(func() { streamTimeoutPolicyProvider = oldPolicyProvider })

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	go func() {
		_, _ = fmt.Fprint(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n")
		time.Sleep(1200 * time.Millisecond)
		_, _ = fmt.Fprint(pw, "data: invalid-json\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: pr}, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
		if data == "invalid-json" {
			sr.Stop(errors.New("invalid upstream event"))
		}
	})

	assert.Empty(t, recorder.Body.String())
	assert.False(t, info.HasValidOutput())
}

func TestStreamScannerHandler_PrecommitEventLimitFailsWithoutMetadataLeak(t *testing.T) {
	var body strings.Builder
	for i := 0; i <= maxPrecommitEvents; i++ {
		fmt.Fprintf(&body, "data: {\"type\":\"response.created\",\"sequence_number\":%d}\n", i)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(body.String()))}, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonHandlerStop, info.StreamStatus.EndReason)
	assert.ErrorIs(t, info.StreamStatus.EndError, errPrecommitBufferExceeded)
	assert.True(t, info.HasValidOutput(), "terminal overflow response must lock out failover")
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "stream_buffer_overflow")
	assert.NotContains(t, recorder.Body.String(), "response.created")
}

func TestStreamScannerHandler_AllowsExactlyPrecommitEventLimit(t *testing.T) {
	var body strings.Builder
	for i := 0; i < maxPrecommitEvents; i++ {
		fmt.Fprintf(&body, "data: {\"type\":\"response.created\",\"sequence_number\":%d}\n", i)
	}
	body.WriteString("data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello\"}\n")
	body.WriteString("data: [DONE]\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(body.String()))}, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
	})

	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.False(t, info.StreamStatus.HasErrors())
	assert.True(t, info.HasValidOutput())
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestStreamScannerHandler_PrecommitByteLimitFails(t *testing.T) {
	payload := strings.Repeat("x", maxPrecommitBytes)
	body := "data: {\"type\":\"response.created\",\"id\":\"" + payload + "\"}\n"

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: io.NopCloser(strings.NewReader(body))}, info, func(data string, sr *StreamResult) {})

	require.NotNil(t, info.StreamStatus)
	assert.ErrorIs(t, info.StreamStatus.EndError, errPrecommitBufferExceeded)
	assert.True(t, info.HasValidOutput())
	assert.Contains(t, recorder.Body.String(), "stream_buffer_overflow")
}

func TestStreamScannerHandler_FirstContentTimeoutDiscardsMetadata(t *testing.T) {
	oldPolicyProvider := streamTimeoutPolicyProvider
	streamTimeoutPolicyProvider = func() streamTimeoutPolicy {
		return streamTimeoutPolicy{
			FirstEvent:   time.Second,
			FirstContent: 50 * time.Millisecond,
			Idle:         time.Second,
			Total:        time.Second,
		}
	}
	t.Cleanup(func() { streamTimeoutPolicyProvider = oldPolicyProvider })

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	go func() {
		_, _ = fmt.Fprint(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: pr}, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.Error(t, info.StreamStatus.EndError)
	assert.Contains(t, info.StreamStatus.EndError.Error(), "first_content")
	assert.False(t, info.HasValidOutput())
	assert.Empty(t, recorder.Body.String())
}

func TestStreamScannerHandler_FirstContentTimeoutStartsAfterFirstEvent(t *testing.T) {
	oldPolicyProvider := streamTimeoutPolicyProvider
	streamTimeoutPolicyProvider = func() streamTimeoutPolicy {
		return streamTimeoutPolicy{
			FirstEvent:   150 * time.Millisecond,
			FirstContent: 100 * time.Millisecond,
			Idle:         500 * time.Millisecond,
			Total:        time.Second,
		}
	}
	t.Cleanup(func() { streamTimeoutPolicyProvider = oldPolicyProvider })

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	go func() {
		time.Sleep(120 * time.Millisecond)
		_, _ = fmt.Fprint(pw, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n")
		time.Sleep(70 * time.Millisecond)
		_, _ = fmt.Fprint(pw, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\ndata: [DONE]\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: pr}, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
	})

	assert.True(t, info.HasValidOutput())
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.Contains(t, recorder.Body.String(), "hello")
}

func TestStreamScannerHandler_FirstEventTimeout(t *testing.T) {
	oldPolicyProvider := streamTimeoutPolicyProvider
	streamTimeoutPolicyProvider = func() streamTimeoutPolicy {
		return streamTimeoutPolicy{
			FirstEvent:   50 * time.Millisecond,
			FirstContent: time.Second,
			Idle:         time.Second,
			Total:        time.Second,
		}
	}
	t.Cleanup(func() { streamTimeoutPolicyProvider = oldPolicyProvider })

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: pr}, info, func(data string, sr *StreamResult) {})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.Error(t, info.StreamStatus.EndError)
	assert.Contains(t, info.StreamStatus.EndError.Error(), "first_event")
	assert.False(t, info.HasValidOutput())
	assert.Empty(t, recorder.Body.String())
}

func TestStreamScannerHandler_IdleTimeoutAfterCommitKeepsSingleResponse(t *testing.T) {
	oldPolicyProvider := streamTimeoutPolicyProvider
	streamTimeoutPolicyProvider = func() streamTimeoutPolicy {
		return streamTimeoutPolicy{
			FirstEvent:   time.Second,
			FirstContent: time.Second,
			Idle:         50 * time.Millisecond,
			Total:        time.Second,
		}
	}
	t.Cleanup(func() { streamTimeoutPolicyProvider = oldPolicyProvider })

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})
	go func() {
		_, _ = fmt.Fprint(pw, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: pr}, info, func(data string, sr *StreamResult) {
		require.NoError(t, StringData(c, data))
	})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.Error(t, info.StreamStatus.EndError)
	assert.Contains(t, info.StreamStatus.EndError.Error(), "idle")
	assert.True(t, info.HasValidOutput())
	assert.Equal(t, 1, strings.Count(recorder.Body.String(), "hello"))
}

func TestStreamScannerHandler_TotalTimeout(t *testing.T) {
	oldPolicyProvider := streamTimeoutPolicyProvider
	streamTimeoutPolicyProvider = func() streamTimeoutPolicy {
		return streamTimeoutPolicy{
			FirstEvent:   time.Second,
			FirstContent: time.Second,
			Idle:         time.Second,
			Total:        50 * time.Millisecond,
		}
	}
	t.Cleanup(func() { streamTimeoutPolicyProvider = oldPolicyProvider })

	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{}}

	StreamScannerHandler(c, &http.Response{Body: pr}, info, func(data string, sr *StreamResult) {})

	require.NotNil(t, info.StreamStatus)
	assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
	require.Error(t, info.StreamStatus.EndError)
	assert.Contains(t, info.StreamStatus.EndError.Error(), "total")
	assert.False(t, info.HasValidOutput())
	assert.Empty(t, recorder.Body.String())
}
