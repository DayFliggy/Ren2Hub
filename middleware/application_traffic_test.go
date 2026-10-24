package middleware

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestApplicationTrafficCountsConsumedBodiesAndWritesOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(RecordApplicationTraffic())
	engine.POST("/echo", func(c *gin.Context) {
		body := make([]byte, 3)
		_, err := io.ReadFull(c.Request.Body, body)
		require.NoError(t, err)
		require.Equal(t, "abc", string(body))
		_, err = c.Writer.Write([]byte("one"))
		require.NoError(t, err)
		_, err = c.Writer.WriteString("two")
		require.NoError(t, err)
	})
	beforeRequest, beforeResponse := common.ApplicationTrafficSnapshot()
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader("abcdefghi")))
	afterRequest, afterResponse := common.ApplicationTrafficSnapshot()
	require.Equal(t, uint64(3), afterRequest-beforeRequest, "unread advertised bytes are not traffic")
	require.Equal(t, uint64(6), afterResponse-beforeResponse)
	require.Equal(t, "onetwo", response.Body.String())
}

type trafficShortWriter struct {
	gin.ResponseWriter
	failure error
}

func (w *trafficShortWriter) Write([]byte) (int, error)       { return 2, w.failure }
func (w *trafficShortWriter) WriteString(string) (int, error) { return 1, w.failure }

type trafficFailingReader struct {
	closed  bool
	failure error
}

func (r *trafficFailingReader) Read(p []byte) (int, error) { copy(p, "abc"); return 3, r.failure }
func (r *trafficFailingReader) Close() error               { r.closed = true; return r.failure }

func TestApplicationTrafficPreservesPartialIOErrorsAndClose(t *testing.T) {
	failure := errors.New("partial I/O")
	reader := &trafficFailingReader{failure: failure}
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Writer = &trafficShortWriter{ResponseWriter: c.Writer, failure: failure} }, RecordApplicationTraffic())
	engine.POST("/partial", func(c *gin.Context) {
		n, err := c.Request.Body.Read(make([]byte, 8))
		require.Equal(t, 3, n)
		require.ErrorIs(t, err, failure)
		require.ErrorIs(t, c.Request.Body.Close(), failure)
		n, err = c.Writer.Write([]byte("abcdef"))
		require.Equal(t, 2, n)
		require.ErrorIs(t, err, failure)
		n, err = c.Writer.WriteString("uvwxyz")
		require.Equal(t, 1, n)
		require.ErrorIs(t, err, failure)
	})
	beforeRequest, beforeResponse := common.ApplicationTrafficSnapshot()
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/partial", reader))
	afterRequest, afterResponse := common.ApplicationTrafficSnapshot()
	require.Equal(t, uint64(3), afterRequest-beforeRequest)
	require.Equal(t, uint64(3), afterResponse-beforeResponse, "only actual partial writes count")
	require.True(t, reader.closed)
}

func TestApplicationTrafficPreservesSSEFlushAndCancellation(t *testing.T) {
	engine := gin.New()
	engine.Use(RecordApplicationTraffic())
	done := make(chan error, 1)
	engine.GET("/sse", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		_, err := c.Writer.WriteString("data: hello\n\n")
		if err != nil {
			done <- err
			return
		}
		if err = http.NewResponseController(c.Writer).Flush(); err != nil {
			done <- err
			return
		}
		<-c.Request.Context().Done()
		done <- c.Request.Context().Err()
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/sse", nil)
	require.NoError(t, err)
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "data: hello\n", line, "flushed bytes arrive before handler finishes")
	cancel()
	require.NoError(t, response.Body.Close())
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(3 * time.Second):
		t.Fatal("request cancellation not forwarded")
	}
}

func TestApplicationTrafficPreservesWebSocketHijack(t *testing.T) {
	engine := gin.New()
	engine.Use(RecordApplicationTraffic())
	done := make(chan error, 1)
	engine.GET("/ws", func(c *gin.Context) {
		connection, err := (&websocket.Upgrader{}).Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			done <- err
			return
		}
		defer connection.Close()
		kind, payload, err := connection.ReadMessage()
		if err == nil {
			err = connection.WriteMessage(kind, payload)
		}
		done <- err
	})
	server := httptest.NewServer(engine)
	defer server.Close()
	beforeRequest, beforeResponse := common.ApplicationTrafficSnapshot()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/ws", nil)
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(3*time.Second)))
	require.NoError(t, connection.WriteMessage(websocket.TextMessage, []byte("message")))
	kind, payload, err := connection.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, kind)
	require.Equal(t, "message", string(payload))
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("websocket handler did not complete")
	}
	afterRequest, afterResponse := common.ApplicationTrafficSnapshot()
	require.Equal(t, beforeRequest, afterRequest, "WebSocket frames are outside HTTP body counters")
	require.Equal(t, beforeResponse, afterResponse)
}
