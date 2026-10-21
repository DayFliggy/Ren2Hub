package volcengine

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestStreamingTTSObservesFirstAudioOutput(t *testing.T) {
	const channelID = 82501
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if _, err := ReceiveMessage(conn); err != nil {
			t.Error(err)
			return
		}
		msg, _ := NewMessage(MsgTypeAudioOnlyServer, MsgTypeFlagNegativeSeq)
		msg.Sequence = -1
		msg.Payload = []byte("audio")
		frame, err := msg.Marshal()
		if err != nil {
			t.Error(err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: "app|token"},
		OriginModelName: "tts-test",
		IsStream:        true,
	}
	service.BeginRouteObservation(c)

	requestURL := "ws" + strings.TrimPrefix(upstream.URL, "http")
	_, apiErr := handleTTSWebSocketResponse(c, requestURL, VolcengineTTSRequest{}, info, "mp3")
	require.Nil(t, apiErr)
	require.Equal(t, "audio", recorder.Body.String())

	service.FinishRouteAttempt(c, info, nil)
	for _, row := range service.RoutingSnapshot().Observations {
		if row.ChannelID == channelID {
			require.Equal(t, 1, row.Samples)
			require.Equal(t, 1, row.LatencySamples, "streamed audio must end the latency sample")
			return
		}
	}
	t.Fatal("stream attempt did not reach route observations")
}
