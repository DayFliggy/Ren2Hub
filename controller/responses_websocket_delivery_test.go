package controller

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type deliveryFaultConn struct {
	net.Conn
	limit int
	after func()
}

func (c *deliveryFaultConn) Write(p []byte) (int, error) {
	if c.limit >= 0 {
		n := min(c.limit, len(p))
		if n == 0 {
			return 0, io.ErrClosedPipe
		}
		written, err := c.Conn.Write(p[:n])
		if err != nil {
			return written, err
		}
		return written, io.ErrClosedPipe
	}
	n, err := c.Conn.Write(p)
	if c.after != nil {
		c.after()
	}
	return n, err
}
func deliveryTestSocket(t *testing.T, limit int, after func()) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	finished := make(chan struct{})
	engine := gin.New()
	engine.GET("/", func(c *gin.Context) {
		conn, err := (&websocket.Upgrader{}).Upgrade(responsesWSTrackingWriter{ResponseWriter: c.Writer}, c.Request, nil)
		if err != nil {
			return
		}
		wire := conn.UnderlyingConn().(*responsesWSDeliveryConn)
		wire.Conn = &deliveryFaultConn{Conn: wire.Conn, limit: limit, after: after}
		wire.enabled.Store(true)
		accepted <- conn
		<-finished
		_ = conn.Close()
	})
	server := httptest.NewServer(engine)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	socket := <-accepted
	t.Cleanup(func() { _ = client.Close(); close(finished); server.Close() })
	return socket, client
}
func TestResponsesWSDeliveryRequiresPayloadAndPreservesSuccessfulTerminal(t *testing.T) {
	for _, tt := range []struct {
		name                                                             string
		limit                                                            int
		terminal, cancelBefore, cancelAfter, wantDelivered, wantTerminal bool
	}{
		{name: "zero byte", limit: 0},
		{name: "frame header only", limit: 4},
		{name: "partial payload", limit: 32, wantDelivered: true},
		{name: "terminal header only", limit: 4, terminal: true},
		{name: "cancel before write", limit: -1, terminal: true, cancelBefore: true},
		{name: "cancel after terminal write", limit: -1, terminal: true, cancelAfter: true, wantDelivered: true, wantTerminal: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var after func()
			if tt.cancelAfter {
				after = cancel
			}
			socket, _ := deliveryTestSocket(t, tt.limit, after)
			s := &responsesWSSession{ctx: ctx, cancel: cancel, client: socket}
			state := &responsesWSCallState{inbox: make(chan responsesWSMessage, 1), controls: make(chan responsesWSControl, 1), done: make(chan struct{})}
			payload := `{"type":"response.output_text.delta","delta":"` + strings.Repeat("hello ", 64) + `"}`
			if tt.terminal {
				payload = `{"type":"response.completed","response":{"id":"resp_delivery","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"` + strings.Repeat("hello ", 64) + `"}]}],"usage":{"input_tokens":12,"output_tokens":34,"total_tokens":46}}}`
			}
			state.inbox <- responsesWSMessage{kind: websocket.TextMessage, body: []byte(payload)}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-mini"}}
			info.SetEstimatePromptTokens(12)
			if tt.cancelBefore {
				cancel()
			}
			apiErr := s.readCall(c, state, responsesWSCreateRequest{}, info)
			require.Equal(t, tt.wantDelivered, info.HasValidOutput())
			require.Equal(t, tt.wantTerminal, state.terminalDelivered.Load())
			if tt.wantTerminal {
				require.Nil(t, apiErr)
				require.True(t, info.StreamStatus.IsNormalEnd())
				require.Equal(t, 34, info.ResponseUsage.CompletionTokens)
			} else {
				require.NotNil(t, apiErr)
				require.NotEqual(t, 34, info.ResponseUsage.CompletionTokens)
			}
		})
	}
}
func TestResponsesWSWireCounterIgnoresHeadersAndControls(t *testing.T) {
	c := &responsesWSDeliveryConn{}
	// A two-byte control payload, then the header of a 130-byte text frame.
	c.observeWritten([]byte{0x89, 2, 'o', 'k', 0x81, 126, 0, 130})
	require.Zero(t, c.written.Load())
	c.observeWritten([]byte("hello"))
	require.EqualValues(t, 5, c.written.Load())
}

func TestResponsesWSTerminalWaitsForWriteAcknowledgement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	written, acknowledge := make(chan struct{}), make(chan struct{})
	socket, peer := deliveryTestSocket(t, -1, func() {
		close(written)
		<-acknowledge
	})
	t.Cleanup(func() {
		select {
		case <-acknowledge:
		default:
			close(acknowledge)
		}
	})
	s := &responsesWSSession{ctx: ctx, cancel: cancel, client: socket}
	state := &responsesWSCallState{inbox: make(chan responsesWSMessage, 1), controls: make(chan responsesWSControl, 1), done: make(chan struct{})}
	payload := []byte(`{"type":"response.completed","response":{"id":"resp_barrier","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"delivered"}]}],"usage":{"input_tokens":12,"output_tokens":34,"total_tokens":46}}}`)
	state.inbox <- responsesWSMessage{kind: websocket.TextMessage, body: payload}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil).WithContext(ctx)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-mini"}}
	done := make(chan *types.NewAPIError, 1)
	go func() { done <- s.readCall(c, state, responsesWSCreateRequest{}, info) }()
	<-written
	_, received, err := peer.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, payload, received)
	// No accounting evidence may be committed while the writer has not yet
	// acknowledged success, even when the peer can already read the frame.
	require.False(t, state.terminalDelivered.Load())
	require.False(t, info.HasValidOutput())
	require.Nil(t, info.ResponseUsage)
	cancel()
	close(acknowledge)
	require.Nil(t, <-done)
	require.True(t, state.terminalDelivered.Load())
	require.True(t, info.StreamStatus.IsNormalEnd())
	require.True(t, info.HasValidOutput())
	require.Equal(t, 34, info.ResponseUsage.CompletionTokens)
}

func TestResponsesWSLocalErrorAcknowledgesCompletionBeforeNextCreate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	socket, peer := deliveryTestSocket(t, -1, nil)
	state := &responsesWSCallState{done: make(chan struct{})}
	s := &responsesWSSession{ctx: ctx, cancel: cancel, client: socket, current: state,
		request: httptest.NewRequest(http.MethodGet, "/v1/responses", nil),
		runner: func(_ *http.Request, _ string, _ func(*gin.Context) *types.NewAPIError) *types.NewAPIError {
			return newResponsesWSInvalidRequestError(errors.New("previous response is unavailable"))
		}}
	body := []byte(`{"type":"response.create","model":"gpt-4o-mini","input":"hello"}`)
	event, streamID, err := parseResponsesWSEnvelope(body)
	require.NoError(t, err)
	s.runRequest(state, body, event, streamID, "local-error")
	_, response, err := peer.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(response), "previous response is unavailable")
	require.True(t, state.terminalDelivered.Load(), "a local error is an acknowledged terminal too")
	select {
	case <-state.done:
	default:
		t.Fatal("request completion must accompany its acknowledged terminal")
	}
	require.True(t, s.tryReserveCurrent(&responsesWSCallState{done: make(chan struct{})}))
}
