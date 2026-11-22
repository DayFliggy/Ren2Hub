package openai

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type realtimeHarness struct {
	client, upstream *websocket.Conn
	done             chan struct{}
	cancel           context.CancelFunc
	totalTokens      int
}

func realtimeTestHarness(t *testing.T, wrap func(net.Conn) net.Conn, targetWrap ...func(net.Conn) net.Conn) *realtimeHarness {
	t.Helper()
	upstreamConn := make(chan *websocket.Conn, 1)
	up := websocket.Upgrader{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err == nil {
			upstreamConn <- conn
		}
	}))
	t.Cleanup(upstream.Close)
	h := &realtimeHarness{done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	h.cancel = cancel
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(h.done)
		if wrap != nil {
			w = &realtimeHijacker{ResponseWriter: w, wrap: wrap}
		}
		client, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer client.Close()
		dialer := *websocket.DefaultDialer
		if len(targetWrap) > 0 {
			dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
				if err != nil {
					return nil, err
				}
				return targetWrap[0](conn), nil
			}
		}
		target, _, err := dialer.Dial("ws"+strings.TrimPrefix(upstream.URL, "http"), nil)
		if err != nil {
			return
		}
		defer target.Close()
		c, _ := gin.CreateTestContext(w)
		c.Request = r.WithContext(ctx)
		_, usage := OpenaiRealtimeHandler(c, &relaycommon.RelayInfo{ClientWs: client, TargetWs: target, UsePrice: true, ChannelMeta: &relaycommon.ChannelMeta{}})
		if usage != nil {
			h.totalTokens = usage.TotalTokens
		}
	}))
	t.Cleanup(gateway.Close)
	var err error
	h.client, _, err = websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
	require.NoError(t, err)
	h.upstream = <-upstreamConn
	t.Cleanup(func() {
		cancel()
		h.client.Close()
		h.upstream.Close()
		select {
		case <-h.done:
		case <-time.After(time.Second):
			t.Error("handler did not stop during cleanup")
		}
	})
	return h
}

type realtimeHijacker struct {
	http.ResponseWriter
	wrap func(net.Conn) net.Conn
}

func (w *realtimeHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, r, e := w.ResponseWriter.(http.Hijacker).Hijack()
	if e == nil {
		c = w.wrap(c)
	}
	return c, r, e
}

func TestRealtimeMessageLimitsCloseBothPeers(t *testing.T) {
	oldClient, oldTarget := constant.MaxRequestBodyMB, constant.StreamScannerMaxBufferMB
	constant.MaxRequestBodyMB = 1
	constant.StreamScannerMaxBufferMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = oldClient; constant.StreamScannerMaxBufferMB = oldTarget })
	for _, fromClient := range []bool{true, false} {
		t.Run(map[bool]string{true: "client", false: "upstream"}[fromClient], func(t *testing.T) {
			h := realtimeTestHarness(t, nil)
			sender, receiver := h.client, h.upstream
			if !fromClient {
				sender, receiver = receiver, sender
			}
			payload := []byte(`{"type":"unknown","padding":"` + strings.Repeat("x", (1<<20)+1) + `"}`)
			_ = sender.WriteMessage(websocket.TextMessage, payload)
			receiver.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, body, err := receiver.ReadMessage()
			require.Error(t, err, "oversized event forwarded: %d bytes", len(body))
			require.Empty(t, body)
			select {
			case <-h.done:
			case <-time.After(time.Second):
				t.Fatal("oversized message did not terminate session")
			}
		})
	}
}

// 升级完成后模拟停止读取的客户端，只有有效的写期限或关闭才能解除阻塞。
type realtimeBlockedConn struct {
	net.Conn
	mu        sync.Mutex
	deadline  time.Time
	closed    chan struct{}
	once      sync.Once
	attempted chan struct{}
}

func (c *realtimeBlockedConn) SetWriteDeadline(d time.Time) error {
	c.mu.Lock()
	c.deadline = d
	c.mu.Unlock()
	return c.Conn.SetWriteDeadline(d)
}
func (c *realtimeBlockedConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}
func (c *realtimeBlockedConn) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "HTTP/1.1") || strings.HasPrefix(string(p), "GET ") {
		return c.Conn.Write(p)
	}
	select {
	case c.attempted <- struct{}{}:
	default:
	}
	c.mu.Lock()
	d := c.deadline
	c.mu.Unlock()
	if !d.IsZero() && d.After(time.Now()) && time.Until(d) <= 31*time.Second {
		return 0, &net.OpError{Op: "write", Net: "tcp", Err: context.DeadlineExceeded}
	}
	<-c.closed
	return 0, net.ErrClosed
}

func TestRealtimeWriteDeadlineStopsBlockedPeer(t *testing.T) {
	for _, toClient := range []bool{true, false} {
		t.Run(map[bool]string{true: "client", false: "upstream"}[toClient], func(t *testing.T) {
			attempted := make(chan struct{}, 1)
			wrap := func(conn net.Conn) net.Conn {
				return &realtimeBlockedConn{Conn: conn, closed: make(chan struct{}), attempted: attempted}
			}
			var h *realtimeHarness
			if toClient {
				h = realtimeTestHarness(t, wrap)
			} else {
				h = realtimeTestHarness(t, nil, wrap)
			}
			sender := h.upstream
			if !toClient {
				sender = h.client
			}
			require.NoError(t, sender.WriteMessage(websocket.TextMessage, []byte(`{"type":"unknown"}`)))
			select {
			case <-attempted:
			case <-time.After(time.Second):
				t.Fatal("no write attempt")
			}
			select {
			case <-h.done:
			case <-time.After(time.Second):
				t.Fatal("missing write deadline left session blocked")
			}
		})
	}
}

func TestRealtimeCancellationWaitsForReaders(t *testing.T) {
	h := realtimeTestHarness(t, nil)
	h.cancel()
	select {
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not stop both readers")
	}
	for _, peer := range []*websocket.Conn{h.client, h.upstream} {
		peer.SetReadDeadline(time.Now().Add(time.Second))
		_, _, err := peer.ReadMessage()
		require.Error(t, err)
		if e, ok := err.(net.Error); ok {
			require.False(t, e.Timeout(), "connection must be closed before handler returns")
		}
	}
}

func TestRealtimeConcurrentSessionState(t *testing.T) {
	h := realtimeTestHarness(t, nil)
	const count = 100
	var workers sync.WaitGroup
	workers.Add(4)
	for _, peer := range []*websocket.Conn{h.client, h.upstream} {
		go func(peer *websocket.Conn) {
			defer workers.Done()
			peer.SetReadDeadline(time.Now().Add(5 * time.Second))
			for i := 0; i < count; i++ {
				if _, _, err := peer.ReadMessage(); err != nil {
					t.Errorf("read event: %v", err)
					return
				}
			}
		}(peer)
	}
	go func() {
		defer workers.Done()
		for i := 0; i < count; i++ {
			if err := h.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update","session":{"tools":[]}}`)); err != nil {
				t.Errorf("client event: %v", err)
				return
			}
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < count; i++ {
			event := `{"type":"session.updated","session":{"input_audio_format":"pcm16","output_audio_format":"pcm16"}}`
			if i%3 == 0 {
				event = `{"type":"response.done","response":{"usage":{"total_tokens":1,"input_tokens":1,"input_token_details":{"text_tokens":1}}}}`
			} else if i%3 == 2 {
				event = `{"type":"unknown"}`
			}
			if err := h.upstream.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
				t.Errorf("upstream event: %v", err)
				return
			}
		}
	}()
	workers.Wait()
	h.client.Close()
	select {
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("concurrent session did not stop")
	}
	require.Equal(t, 34, h.totalTokens, "all completed responses must remain in the final usage")
}

// 人为延迟一个已进入处理阶段的 reader，验证返回值与 info 不再被退出后的 reader 改写。
type realtimeHeldWriteConn struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
}

func (c *realtimeHeldWriteConn) Write(p []byte) (int, error) {
	if strings.HasPrefix(string(p), "HTTP/1.1") || strings.HasPrefix(string(p), "GET ") {
		return c.Conn.Write(p)
	}
	close(c.entered)
	<-c.release
	return 0, net.ErrClosed
}
func TestRealtimeShutdownWaitsForActiveEvent(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	h := realtimeTestHarness(t, func(conn net.Conn) net.Conn {
		return &realtimeHeldWriteConn{Conn: conn, entered: entered, release: release}
	})
	defer unblock()
	require.NoError(t, h.upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.updated","session":{"input_audio_format":"pcm16"}}`)))
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("event did not enter forwarding")
	}
	h.client.Close()
	select {
	case <-h.done:
		t.Fatal("handler returned while a reader was still processing shared state")
	case <-time.After(100 * time.Millisecond):
	}
	unblock()
	select {
	case <-h.done:
	case <-time.After(time.Second):
		t.Fatal("handler did not join readers")
	}
}
