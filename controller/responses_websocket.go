package controller

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

type responsesWSRequestContextKey struct{}

type responsesWSRequestState struct {
	requestID string
	handle    func(*gin.Context) *types.NewAPIError
	apiError  *types.NewAPIError
}

// Each response.create runs the ordinary request middleware to completion,
// without routing another HTTP request or retaining a pooled Gin context.
var responsesWSRequestEngine = sync.OnceValue(func() *gin.Engine {
	engine := gin.New()
	engine.ForwardedByClientIP = false
	_ = engine.SetTrustedProxies(nil)
	engine.POST("/v1/responses", func(c *gin.Context) {
		state := c.Request.Context().Value(responsesWSRequestContextKey{}).(*responsesWSRequestState)
		c.Set(common.RequestIdKey, state.requestID)
		common.SetContextKey(c, constant.ContextKeyRequestStartTime, time.Now())
		c.Next()
	}, middleware.BodyStorageCleanup(), middleware.TokenAuth(), middleware.ModelRequestRateLimit(), middleware.GatewayAdmission(), func(c *gin.Context) {
		state := c.Request.Context().Value(responsesWSRequestContextKey{}).(*responsesWSRequestState)
		state.apiError = state.handle(c)
		if state.apiError != nil {
			status := state.apiError.StatusCode
			if status < http.StatusBadRequest {
				status = http.StatusInternalServerError
			}
			c.Status(status)
		}
	})
	return engine
})

type responsesWSResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *responsesWSResponseWriter) Header() http.Header {
	return w.header
}

func (w *responsesWSResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *responsesWSResponseWriter) Write(data []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(data)
}

func newResponsesWSRequestRunner(c *gin.Context) ResponsesWSRequestRunner {
	// Capture credentials before channel selection or header overrides. Resolve
	// the peer once with the public router's trusted-proxy configuration.
	headers := c.Request.Header.Clone()
	for _, name := range []string{"Connection", "Upgrade", "Sec-WebSocket-Key", "Sec-WebSocket-Version", "Sec-WebSocket-Extensions", "Sec-WebSocket-Protocol", "Content-Length", "Content-Encoding"} {
		headers.Del(name)
	}
	remoteAddr := net.JoinHostPort(c.ClientIP(), "0")
	return func(request *http.Request, requestID string, handle func(*gin.Context) *types.NewAPIError) *types.NewAPIError {
		state := &responsesWSRequestState{requestID: requestID, handle: handle}
		ctx := context.WithValue(request.Context(), responsesWSRequestContextKey{}, state)
		ctx = context.WithValue(ctx, common.RequestIdKey, requestID)
		request = request.Clone(ctx)
		request.Method = http.MethodPost
		request.URL.Path = "/v1/responses"
		request.URL.RawPath = ""
		request.Header = headers.Clone()
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = remoteAddr
		response := &responsesWSResponseWriter{header: make(http.Header)}
		responsesWSRequestEngine().ServeHTTP(response, request)
		if state.apiError != nil {
			return state.apiError
		}
		if response.status < http.StatusBadRequest {
			return nil
		}
		var body struct {
			Error *types.OpenAIError `json:"error"`
		}
		if common.Unmarshal(response.body.Bytes(), &body) == nil && body.Error != nil {
			return types.WithOpenAIError(*body.Error, response.status, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		// A rejection without an OpenAI error body keeps its HTTP status text.
		return types.NewErrorWithStatusCode(errors.New(http.StatusText(response.status)), types.ErrorCodeInvalidRequest, response.status, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
}

func ResponsesWebSocket(c *gin.Context) {
	requestID := c.GetString(common.RequestIdKey)
	runner := newResponsesWSRequestRunner(c)
	tracked, finish, err := service.TrackGatewayRequest(c.Request.Context())
	if err != nil {
		c.JSON(service.GatewayAdmissionHTTPStatus(err), gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	defer finish()
	c.Request = c.Request.WithContext(tracked)
	wsUpgrader := upgrader
	wsUpgrader.Subprotocols = []string{"responses"}
	ws, err := wsUpgrader.Upgrade(responsesWSTrackingWriter{ResponseWriter: c.Writer}, c.Request, nil)
	if err != nil {
		return
	}
	ws.UnderlyingConn().(*responsesWSDeliveryConn).enabled.Store(true)
	defer ws.Close()
	service.TrackGatewayConnection(tracked, ws.UnderlyingConn())

	if apiError := ResponsesWebSocketHelper(c, ws, runner); apiError != nil {
		logger.LogError(c, fmt.Sprintf("responses websocket relay error: %s", common.LocalLogPreview(apiError.MaskSensitiveError(common.UpstreamAddresses(c)...))))
		apiError.SetMessage(common.MessageWithRequestId(apiError.Error(), requestID))
		helper.WssError(c, ws, apiError.ToOpenAIError(common.UpstreamAddresses(c)...))
	}
}

// Count actual network writes, including a partially delivered large WS frame.
// Buffered application writes alone are not delivery evidence.
type responsesWSDeliveryConn struct {
	net.Conn
	written   atomic.Int64
	enabled   atomic.Bool
	header    []byte
	remaining uint64
	dataFrame bool
}

func (c *responsesWSDeliveryConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if c.enabled.Load() {
		c.observeWritten(p[:n])
	}
	return n, err
}

// Gorilla serializes wire writes. Keep header state across partial writes and
// count payload only; ping/pong/close traffic never acknowledges generation.
func (c *responsesWSDeliveryConn) observeWritten(p []byte) {
	for len(p) > 0 {
		if c.remaining > 0 {
			n := min(uint64(len(p)), c.remaining)
			if c.dataFrame {
				c.written.Add(int64(n))
			}
			c.remaining -= n
			p = p[n:]
			continue
		}
		c.header = append(c.header, p[0])
		p = p[1:]
		if len(c.header) < 2 {
			continue
		}
		length := uint64(c.header[1] & 127)
		needed := 2
		if length == 126 {
			needed += 2
		} else if length == 127 {
			needed += 8
		}
		if c.header[1]&128 != 0 {
			needed += 4
		}
		if len(c.header) < needed {
			continue
		}
		if length == 126 {
			length = uint64(binary.BigEndian.Uint16(c.header[2:4]))
		} else if length == 127 {
			length = binary.BigEndian.Uint64(c.header[2:10])
		}
		opcode := c.header[0] & 15
		c.dataFrame = opcode == websocket.TextMessage || opcode == websocket.BinaryMessage || opcode == 0
		c.remaining = length
		c.header = c.header[:0]
	}
}

type responsesWSTrackingWriter struct{ gin.ResponseWriter }

func (w responsesWSTrackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, buffer, err := w.ResponseWriter.Hijack()
	if err != nil {
		return nil, nil, err
	}
	return &responsesWSDeliveryConn{Conn: conn}, buffer, nil
}
