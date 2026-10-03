package router

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestStandardOpenAICodexCompactAndWebSocket(t *testing.T) {
	var sends atomic.Int32
	accepted := make(chan *websocket.Conn, 1)
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		assert.Equal(t, "Bearer standard-codex-api-fixture", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Chatgpt-Account-Id"))
		if r.URL.Path == "/v1/responses/compact" {
			body, err := io.ReadAll(r.Body)
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			assert.Equal(t, "gpt-5-codex", gjson.GetBytes(body, "model").String())
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"compact_codex_api","object":"response.compaction","output":[{"type":"compaction","id":"cmp_codex_api","encrypted_content":"codex_api_cipher"}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`)
			return
		}
		assert.Equal(t, "/v1/responses", r.URL.Path)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if assert.NoError(t, err) {
			accepted <- conn
		}
	})
	channel, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	channel.Type = constant.ChannelTypeOpenAI
	channel.Models = "gpt-5-codex"
	channel.Key = "standard-codex-api-fixture"
	channel.Setting = common.GetPointer(`{"responses_websocket_enabled":true}`)
	require.NoError(t, channel.Update())
	require.NoError(t, configureGatewayFixturePrices(`{"gpt-5-codex":0.001,"gpt-5-codex-openai-compact":0.001}`, false))
	require.NoError(t, service.InitRouteCapabilityIndex(t.Context()))
	status, body := gatewayStateHTTP(t, base, key, "/v1/responses/compact", `{"model":"gpt-5-codex","input":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "codex_api_cipher")
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer " + key}})
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-5-codex","input":"hello","max_output_tokens":10,"store":false}`)))
	var upstream *websocket.Conn
	select {
	case upstream = <-accepted:
		defer upstream.Close()
	case <-time.After(3 * time.Second):
		_ = client.SetReadDeadline(time.Now().Add(time.Second))
		_, payload, readErr := client.ReadMessage()
		t.Fatalf("no standard OpenAI WebSocket handshake: %s %v", payload, readErr)
	}
	require.NoError(t, upstream.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, wire, err := upstream.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, "gpt-5-codex", gjson.GetBytes(wire, "model").String())
	assert.Equal(t, "false", gjson.GetBytes(wire, "store").Raw)
	const terminal = `{"type":"response.completed","response":{"id":"ws_codex_api","status":"completed","model":"gpt-5-codex","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`
	require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(terminal)))
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, result, err := client.ReadMessage()
	require.NoError(t, err)
	assert.JSONEq(t, terminal, string(result))
	awaitGatewayPermitsReleased(t)
	assert.EqualValues(t, 2, sends.Load())
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Zero(t, user.ReservedQuota)
	assert.Positive(t, user.UsedQuota)
}
