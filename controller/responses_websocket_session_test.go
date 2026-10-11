package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSEnvelopeSeparatesControlsAndPreservesZero(t *testing.T) {
	body := []byte(`{"type":"response.create","stream_id":"one","generate":false,"response":{"model":"test","temperature":0,"top_p":0,"stream_id":"shadow","background":true,"stream":true,"stream_options":{"include_usage":true}}}`)
	event, id, err := parseResponsesWSEnvelope(body)
	require.NoError(t, err)
	create, err := normalizeResponsesWSCreateEvent(body, event, id)
	require.NoError(t, err)
	require.NotNil(t, create.Request.Temperature)
	assert.Zero(t, *create.Request.Temperature)
	assert.NotContains(t, string(create.Body), "stream_id")
	assert.NotContains(t, string(create.Body), "background")
	assert.NotContains(t, string(create.Body), "stream")
	payload, err := buildResponsesWSCreateEvent(create.Body, create.Generate, id)
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, common.Unmarshal(payload, &wire))
	assert.Equal(t, "one", wire["stream_id"])
	assert.Equal(t, false, wire["generate"])
	for _, raw := range []string{`{"type":"response.create","stream_id":42}`, `{"type":"response.create","stream_id":"x/y"}`, `{"type":"response.create","stream_id":""}`} {
		_, _, err := parseResponsesWSEnvelope([]byte(raw))
		require.Error(t, err)
	}
	event.Generate = []byte(`"false"`)
	_, err = normalizeResponsesWSCreateEvent(body, event, id)
	require.ErrorContains(t, err, "boolean")
}

func TestResponsesWSCancelErrorDoesNotEndGeneration(t *testing.T) {
	control := []byte(`{"type":"response.cancel","event_id":"cancel-one","stream_id":"stream-one","response_id":"resp-one"}`)
	event := responsesWSErrorEvent{Type: "error", EventID: "cancel-one", StreamID: "stream-one", Error: &types.OpenAIError{Type: "invalid_request_error"}}
	terminal, ambiguous, controlError := responsesWSErrorEndsRequest(event, "stream-one", "resp-one", control)
	assert.False(t, terminal)
	assert.False(t, ambiguous)
	assert.True(t, controlError)
	event.EventID = ""
	event.StreamID = "other"
	terminal, _, _ = responsesWSErrorEndsRequest(event, "stream-one", "resp-one", control)
	assert.False(t, terminal)
	event.StreamID = "stream-one"
	terminal, ambiguous, _ = responsesWSErrorEndsRequest(event, "stream-one", "resp-one", control)
	assert.True(t, terminal)
	assert.True(t, ambiguous)
}

// newResponsesWSTestPair returns the two ends of one WebSocket connection.
func newResponsesWSTestPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if !assert.NoError(t, err) {
			return
		}
		accepted <- conn
	}))
	t.Cleanup(server.Close)
	dialed, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	peer := <-accepted
	t.Cleanup(func() { _ = dialed.Close(); _ = peer.Close() })
	return dialed, peer
}

func TestResponsesWSShutdownSendsCloseFramesWithoutWaitingForWriters(t *testing.T) {
	client, clientPeer := newResponsesWSTestPair(t)
	target, targetPeer := newResponsesWSTestPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	s := &responsesWSSession{ctx: ctx, cancel: cancel, client: client, target: target}
	// Blocked network writers own these locks. Shutdown must not wait for
	// them: its close frames are bounded control writes, and closing the
	// sockets is what interrupts those writers.
	s.clientWriteMu.Lock()
	defer s.clientWriteMu.Unlock()
	s.targetWriteMu.Lock()
	defer s.targetWriteMu.Unlock()
	done := make(chan struct{})
	go func() { s.shutdown(websocket.CloseInternalServerErr, "upstream connection closed"); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown waited for a network writer")
	}
	// A dropped socket would read as a synthetic 1006; 1011 is the frame.
	for _, conn := range []*websocket.Conn{clientPeer, targetPeer} {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
		_, _, err := conn.ReadMessage()
		assert.True(t, websocket.IsCloseError(err, websocket.CloseInternalServerErr), "want a 1011 close frame, got %v", err)
	}
	assert.Nil(t, s.getTarget())

	// An upstream dial that completes after shutdown is closed with a frame too.
	late, latePeer := newResponsesWSTestPair(t)
	assert.False(t, s.setTarget(late))
	require.NoError(t, latePeer.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := latePeer.ReadMessage()
	assert.True(t, websocket.IsCloseError(err, websocket.CloseNormalClosure), "want a 1000 close frame, got %v", err)
}
