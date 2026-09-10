package xunfei

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/execution"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestXunfeiSharesHTTPBudgetAndJoinsReaderOnCancel(t *testing.T) {
	service.InitHttpClient()
	var calls atomic.Int32
	emitted := make(chan error, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if !websocket.IsWebSocketUpgrade(request) {
			_, _ = io.Copy(io.Discard, request.Body)
			writer.WriteHeader(http.StatusOK)
			return
		}
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			emitted <- err
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil {
			emitted <- err
			return
		}
		emitted <- conn.WriteMessage(websocket.TextMessage, []byte(`{"payload":{"choices":{"status":1,"text":[{"content":"fixture"}]}}}`))
		_, _, _ = conn.ReadMessage()
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 2}}
	state := channel.RequestExecution(c, execution.Generation)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, strings.NewReader(`{}`))
	require.NoError(t, err)
	response, err := channel.DoRequest(c, req, info)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	for index := 0; index < 2; index++ {
		_, _, cleanup, err := xunfeiMakeRequest(c, info, dto.GeneralOpenAIRequest{}, "general", "ws"+strings.TrimPrefix(upstream.URL, "http"), "fixture-app")
		require.NoError(t, err)
		require.NoError(t, <-emitted)
		finished := make(chan struct{})
		go func() { cleanup(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Fatal("reader waiting for its consumer did not stop")
		}
	}
	_, _, _, err = xunfeiMakeRequest(c, info, dto.GeneralOpenAIRequest{}, "general", "ws"+strings.TrimPrefix(upstream.URL, "http"), "fixture-app")
	require.ErrorIs(t, err, execution.ErrCallBudget)
	require.Equal(t, 3, state.CallCount())
	require.EqualValues(t, 3, calls.Load())
}
