package common

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// serveCounted runs a real http.Server on a counted loopback listener.
func serveCounted(t *testing.T, handler http.Handler) (*TrafficCounter, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	counter := &TrafficCounter{}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(counter.Listener(ln)) }()
	t.Cleanup(func() { _ = server.Close() })
	return counter, ln.Addr().String()
}

// requireCounts waits for the server side to record what the client saw; the
// server counts a write just after the client may already have received it.
func requireCounts(t *testing.T, counter *TrafficCounter, in, out uint64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		gotIn, gotOut := counter.Snapshot()
		if gotIn == in && gotOut == out {
			return
		}
		if time.Now().After(deadline) {
			require.Equal(t, [2]uint64{in, out}, [2]uint64{gotIn, gotOut}, "server in/out bytes")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// echoTenfold serves the body through http.ServeContent, whose io.CopyN uses
// ReadFrom on the connection when one is promoted; that path would skip Write.
func echoTenfold(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(bytes.Repeat(body, 10)))
}

// exchange sends bodies as sequential requests on one client connection and
// returns the bytes it wrote and read. The last request closes the connection,
// and the server's close is read to EOF.
func exchange(addr string, bodies []string) (sent, received int64, err error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return 0, 0, err
	}
	defer conn.Close()
	if err = conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, 0, err
	}
	var raw bytes.Buffer
	reader := bufio.NewReader(io.TeeReader(conn, &raw))
	for i, body := range bodies {
		closing := ""
		if i == len(bodies)-1 {
			closing = "Connection: close\r\n"
		}
		request := fmt.Sprintf("POST /echo HTTP/1.1\r\nHost: test\r\n%sContent-Length: %d\r\n\r\n%s", closing, len(body), body)
		n, err := io.WriteString(conn, request)
		sent += int64(n)
		if err != nil {
			return sent, int64(raw.Len()), err
		}
		response, err := http.ReadResponse(reader, nil)
		if err != nil {
			return sent, int64(raw.Len()), fmt.Errorf("response %d on the reused connection: %w", i, err)
		}
		echoed, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || len(echoed) != 10*len(body) {
			return sent, int64(raw.Len()), fmt.Errorf("response %d: %d body bytes, err %v", i, len(echoed), err)
		}
	}
	if _, err = reader.ReadByte(); !errors.Is(err, io.EOF) {
		return sent, int64(raw.Len()), fmt.Errorf("expected the server to close, got %v", err)
	}
	return sent, int64(raw.Len()), nil
}

func TestCountingListenerCountsHeadersAndBodiesAcrossKeepAlive(t *testing.T) {
	counter, addr := serveCounted(t, http.HandlerFunc(echoTenfold))
	bodies := []string{strings.Repeat("a", 600), strings.Repeat("b", 700), strings.Repeat("c", 800)}
	sent, received, err := exchange(addr, bodies)
	require.NoError(t, err)
	require.Greater(t, received, int64(21000), "headers are counted as well as the 21000 body bytes")
	requireCounts(t, counter, uint64(sent), uint64(received))
}

func TestCountingListenerCountsWebSocketFramesAfterHandshake(t *testing.T) {
	upgraded, handled := make(chan struct{}), make(chan error, 1)
	// ReadBufferSize 0 makes gorilla reuse the reader net/http hijacked.
	upgrader := websocket.Upgrader{ReadBufferSize: 0}
	counter, addr := serveCounted(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			handled <- err
			return
		}
		defer conn.Close()
		close(upgraded)
		_, payload, err := conn.ReadMessage()
		if err == nil {
			err = conn.WriteMessage(websocket.TextMessage, append(payload, payload...))
		}
		handled <- err
	}))
	client, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/ws", nil)
	require.NoError(t, err)
	defer client.Close()
	<-upgraded
	baseIn, baseOut := counter.Snapshot()

	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte("message")))
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, payload, err := client.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "messagemessage", string(payload))
	require.NoError(t, <-handled)
	in, out := counter.Snapshot()
	// A masked client frame is 2+4+7 bytes; the unmasked reply is 2+14.
	require.Equal(t, uint64(13), in-baseIn, "frame bytes read after the handshake")
	require.Equal(t, uint64(16), out-baseOut, "frame bytes written after the handshake")
}

func TestCountingListenerKeepsSSEFlush(t *testing.T) {
	release, finished := make(chan struct{}), make(chan struct{})
	counter, addr := serveCounted(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\n")
		if err := http.NewResponseController(w).Flush(); err != nil {
			return
		}
		<-release
		_, _ = io.WriteString(w, "data: last\n\n")
	}))
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/sse", nil)
	require.NoError(t, err)
	response, err := transport.RoundTrip(request)
	require.NoError(t, err)
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: first\n", line)
	select {
	case <-finished:
		t.Fatal("the first event arrived only after the handler returned")
	default:
	}
	_, out := counter.Snapshot()
	require.Positive(t, out, "flushed bytes are counted before the handler returns")
	close(release)
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "\ndata: last\n\n", string(rest))
}

// countedPair returns the accepted end of a real TCP connection, counted, and
// the dialing end.
func countedPair(t *testing.T) (net.Conn, net.Conn, *TrafficCounter) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	counter := &TrafficCounter{}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := counter.Listener(ln).Accept()
		if err == nil {
			accepted <- conn
		}
		close(accepted)
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)
	server, ok := <-accepted
	require.True(t, ok, "accept failed")
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	require.NoError(t, client.SetDeadline(time.Now().Add(3*time.Second)))
	require.NoError(t, server.SetDeadline(time.Now().Add(3*time.Second)))
	return server, client, counter
}

func TestCountingConnCloseWriteSendsFINAndKeepsReading(t *testing.T) {
	server, client, counter := countedPair(t)
	_, err := io.WriteString(server, "bye")
	require.NoError(t, err)
	closer, ok := server.(interface{ CloseWrite() error })
	require.True(t, ok)
	require.NoError(t, closer.CloseWrite())
	got, err := io.ReadAll(client)
	require.NoError(t, err, "the peer reads to EOF after the FIN")
	require.Equal(t, "bye", string(got))

	_, err = io.WriteString(client, "still open")
	require.NoError(t, err)
	buffer := make([]byte, 10)
	_, err = io.ReadFull(server, buffer)
	require.NoError(t, err, "the half-closed side keeps reading")
	require.Equal(t, "still open", string(buffer))
	requireCounts(t, counter, 10, 3)

	// A connection without half-close reports it instead of a fake success.
	pipe, peer := net.Pipe()
	defer pipe.Close()
	defer peer.Close()
	err = (&countingConn{Conn: pipe, counter: &TrafficCounter{}}).CloseWrite()
	require.ErrorIs(t, err, errors.ErrUnsupported)
}

// partialConn returns a short count together with an error.
type partialConn struct {
	net.Conn
	n   int
	err error
}

func (c *partialConn) Read(p []byte) (int, error)  { return copy(p, "abcdef"[:c.n]), c.err }
func (c *partialConn) Write(p []byte) (int, error) { return c.n, c.err }

func TestCountingConnCountsPartialIOAndPreservesErrors(t *testing.T) {
	failure := errors.New("partial I/O")
	counter := &TrafficCounter{}
	conn := &countingConn{Conn: &partialConn{n: 3, err: failure}, counter: counter}
	n, err := conn.Read(make([]byte, 8))
	require.Equal(t, 3, n)
	require.Same(t, failure, err, "the error is returned unchanged")
	n, err = conn.Write([]byte("abcdef"))
	require.Equal(t, 3, n)
	require.Same(t, failure, err)
	conn.Conn = &partialConn{n: 0, err: io.EOF}
	n, err = conn.Read(make([]byte, 8))
	require.Zero(t, n)
	require.Same(t, io.EOF, err)
	in, out := counter.Snapshot()
	require.Equal(t, [2]uint64{3, 3}, [2]uint64{in, out}, "only bytes actually moved are counted")

	server, client, counter := countedPair(t)
	require.NoError(t, server.SetReadDeadline(time.Now().Add(-time.Second)))
	_, err = server.Read(make([]byte, 1))
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "read deadlines reach the socket")
	require.NoError(t, server.SetReadDeadline(time.Time{}))
	require.NoError(t, server.SetWriteDeadline(time.Now().Add(-time.Second)))
	_, err = server.Write([]byte("x"))
	require.ErrorIs(t, err, os.ErrDeadlineExceeded, "write deadlines reach the socket")
	require.NoError(t, server.SetDeadline(time.Now().Add(3*time.Second)))
	_, err = io.WriteString(client, "ok")
	require.NoError(t, err)
	_, err = io.ReadFull(server, make([]byte, 2))
	require.NoError(t, err, "a cleared deadline reads again")
	requireCounts(t, counter, 2, 0)
}

func TestCountingListenerShutdownDrainsAndReturnsServerClosed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = io.WriteString(w, "drained")
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve((&TrafficCounter{}).Listener(ln)) }()
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	type result struct {
		body string
		err  error
	}
	responses := make(chan result, 1)
	go func() {
		request, _ := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/slow", nil)
		response, err := transport.RoundTrip(request)
		if err != nil {
			responses <- result{err: err}
			return
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		responses <- result{string(body), err}
	}()
	<-entered
	shutdown := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdown <- server.Shutdown(ctx)
	}()
	select {
	case err := <-served:
		require.ErrorIs(t, err, http.ErrServerClosed, "Shutdown unblocks Accept on the wrapped listener")
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after Shutdown")
	}
	select {
	case err := <-shutdown:
		t.Fatalf("Shutdown returned before the in-flight request finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	got := <-responses
	require.NoError(t, got.err)
	require.Equal(t, "drained", got.body)
	require.NoError(t, <-shutdown)
}

func TestCountingListenerConcurrentConnections(t *testing.T) {
	counter, addr := serveCounted(t, http.HandlerFunc(echoTenfold))
	const workers, requests = 8, 10
	var group sync.WaitGroup
	sent, received, failures := make([]int64, workers), make([]int64, workers), make([]error, workers)
	for worker := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			bodies := make([]string, requests)
			for i := range bodies {
				bodies[i] = strings.Repeat(string(rune('a'+worker)), 520+worker*13+i)
			}
			sent[worker], received[worker], failures[worker] = exchange(addr, bodies)
		}()
	}
	stop, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		var lastIn, lastOut uint64
		for {
			select {
			case <-stop:
				return
			default:
			}
			in, out := counter.Snapshot()
			if in < lastIn || out < lastOut {
				t.Errorf("counters went backwards: %d/%d after %d/%d", in, out, lastIn, lastOut)
				return
			}
			lastIn, lastOut = in, out
			time.Sleep(time.Millisecond)
		}
	}()
	group.Wait()
	close(stop)
	<-sampled
	var totalSent, totalReceived int64
	for worker := range workers {
		require.NoError(t, failures[worker], "worker %d", worker)
		totalSent += sent[worker]
		totalReceived += received[worker]
	}
	requireCounts(t, counter, uint64(totalSent), uint64(totalReceived))
}
