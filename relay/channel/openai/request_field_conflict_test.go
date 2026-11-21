package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestRealtimeRejectsDuplicateFieldsWithoutForwarding(t *testing.T) {
	upstreamFrames := make(chan string, 4)
	upgrader := websocket.Upgrader{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			upstreamFrames <- string(message)
		}
	}))
	defer upstream.Close()
	finished := make(chan struct{})
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(finished)
		client, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer client.Close()
		target, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(upstream.URL, "http"), nil)
		if err != nil {
			return
		}
		defer target.Close()
		c, _ := gin.CreateTestContext(w)
		c.Request = r
		OpenaiRealtimeHandler(c, &relaycommon.RelayInfo{ClientWs: client, TargetWs: target, ChannelMeta: &relaycommon.ChannelMeta{}})
	}))
	defer gateway.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(gateway.URL, "http"), nil)
	require.NoError(t, err)
	defer client.Close()
	for _, frame := range []string{
		`{"type":"response.create","Type":"response.cancel"}`,
		`{"type":"response.create","response":{"modalities":["text"],"modalities":["audio"]}}`,
	} {
		require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(frame)))
		require.NoError(t, client.SetReadDeadline(time.Now().Add(2*time.Second)))
		_, reply, err := client.ReadMessage()
		require.NoError(t, err)
		var event struct {
			Type  string                               `json:"type"`
			Error struct{ Type, Code, Message string } `json:"error"`
		}
		require.NoError(t, common.Unmarshal(reply, &event))
		require.Equal(t, "error", event.Type)
		require.Equal(t, "invalid_request_error", event.Error.Type)
		require.Equal(t, "duplicate_request_field", event.Error.Code)
		require.Contains(t, event.Error.Message, "Duplicate request field")
	}
	const valid = `{"type":"response.cancel"}`
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(valid)))
	select {
	case frame := <-upstreamFrames:
		require.Equal(t, valid, frame, "upstream must receive only the subsequent valid frame")
	case <-time.After(2 * time.Second):
		t.Fatal("valid frame was not forwarded")
	}
	require.NoError(t, client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")))
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not stop")
	}
}
