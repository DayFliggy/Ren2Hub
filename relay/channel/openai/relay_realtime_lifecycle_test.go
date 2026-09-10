package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func realtimeSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	type accepted struct {
		conn *websocket.Conn
		err  error
	}
	ready := make(chan accepted, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(writer, request, nil)
		ready <- accepted{conn, err}
	}))
	t.Cleanup(server.Close)
	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	result := <-ready
	require.NoError(t, result.err)
	t.Cleanup(func() { _ = peer.Close(); _ = result.conn.Close() })
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(3*time.Second)))
	return result.conn, peer
}

func TestRealtimeShutdownJoinsBothReadersAndKeepsUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, termination := range []string{"request cancellation", "client close", "upstream close"} {
		t.Run(termination, func(t *testing.T) {
			clientConn, clientPeer := realtimeSocketPair(t)
			targetConn, targetPeer := realtimeSocketPair(t)
			requestContext, cancel := context.WithCancel(context.Background())
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil).WithContext(requestContext)
			info := &relaycommon.RelayInfo{
				ClientWs: clientConn, TargetWs: targetConn,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "realtime-test-model"},
			}
			info.UsePrice = true
			type result struct {
				err   *types.NewAPIError
				usage *dto.RealtimeUsage
			}
			finished := make(chan result, 1)
			go func() {
				err, usage := OpenaiRealtimeHandler(c, info)
				finished <- result{err, usage}
			}()

			clientEvent := []byte(`{"type":"session.update","session":{"tools":[]},"extension":false}`)
			upstreamEvent := []byte(`{"type":"session.created","session":{"input_audio_format":"pcm16","output_audio_format":"g711_ulaw"}}`)
			require.NoError(t, clientPeer.WriteMessage(websocket.TextMessage, clientEvent))
			require.NoError(t, targetPeer.WriteMessage(websocket.TextMessage, upstreamEvent))
			_, forwardedClient, err := targetPeer.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, clientEvent, forwardedClient)
			_, forwardedUpstream, err := clientPeer.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, upstreamEvent, forwardedUpstream)

			completion := []byte(`{"type":"response.done","response":{"usage":{"total_tokens":17,"input_tokens":10,"output_tokens":7,"input_token_details":{"text_tokens":6,"audio_tokens":4},"output_token_details":{"text_tokens":2,"audio_tokens":5}}}}`)
			require.NoError(t, targetPeer.WriteMessage(websocket.TextMessage, completion))
			_, forwardedCompletion, err := clientPeer.ReadMessage()
			require.NoError(t, err)
			require.Equal(t, completion, forwardedCompletion)
			switch termination {
			case "request cancellation":
				cancel()
			case "client close":
				require.NoError(t, clientPeer.Close())
			case "upstream close":
				require.NoError(t, targetPeer.Close())
			}
			select {
			case result := <-finished:
				require.Nil(t, result.err)
				require.NotNil(t, result.usage)
				require.Equal(t, 17, result.usage.TotalTokens)
				require.Equal(t, 10, result.usage.InputTokens)
				require.Equal(t, 7, result.usage.OutputTokens)
				require.Equal(t, 4, result.usage.InputTokenDetails.AudioTokens)
				require.Equal(t, 5, result.usage.OutputTokenDetails.AudioTokens)
			case <-time.After(2 * time.Second):
				t.Fatal("realtime handler did not finish after cancellation or peer close")
			}
			require.Equal(t, "pcm16", info.InputAudioFormat)
			require.Equal(t, "g711_ulaw", info.OutputAudioFormat)
			_, _, err = clientPeer.ReadMessage()
			require.Error(t, err)
			_, _, err = targetPeer.ReadMessage()
			require.Error(t, err)
		})
	}
}

func TestRealtimeMissingResponseReturnsProtocolError(t *testing.T) {
	state := realtimeRelayState{info: &relaycommon.RelayInfo{}}
	err := state.recordTargetEvent(nil, &dto.RealtimeEvent{Type: dto.RealtimeEventTypeResponseDone})
	require.ErrorContains(t, err, "missing response")
	require.Zero(t, state.total.TotalTokens)
}
