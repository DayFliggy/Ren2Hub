package controller

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestResponsesWSSUsesHTTP1ThroughProxyWithTLS(t *testing.T) {
	protocols := make(chan int, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		protocols <- r.ProtoMajor
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		kind, payload, err := conn.ReadMessage()
		if err == nil {
			_ = conn.WriteMessage(kind, payload)
		}
	}))
	upstream.EnableHTTP2 = true
	upstream.StartTLS()
	defer upstream.Close()
	var connects atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" {
			http.Error(w, "CONNECT required", 405)
			return
		}
		connects.Add(1)
		target, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			http.Error(w, err.Error(), 502)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = target.Close()
			return
		}
		defer client.Close()
		defer target.Close()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		copied := make(chan struct{})
		go func() { _, _ = io.Copy(target, client); _ = target.Close(); close(copied) }()
		_, _ = io.Copy(client, target)
		_ = client.Close()
		<-copied
	}))
	defer proxy.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	request, err := http.NewRequest("GET", upstream.URL+"/v1/responses", nil)
	require.NoError(t, err)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelSetting: dto.ChannelSettings{HTTPProtocol: dto.HTTPProtocolAuto, HTTP2ConnectionShards: 2, Proxy: proxy.URL, TLSInsecureSkipVerify: true}}}
	conn, err := dialResponsesWebSocket(c, info, request)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("fixture")))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, body, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "fixture", string(body))
	require.Equal(t, 1, <-protocols)
	require.EqualValues(t, 1, connects.Load())
}
