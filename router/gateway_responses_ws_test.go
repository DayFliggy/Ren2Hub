package router

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayResponsesWSFinalQuoteAndPersistentConnection(t *testing.T) {
	var handshakes atomic.Int32
	accepted := make(chan *websocket.Conn, 2)
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		handshakes.Add(1)
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Update("setting", `{"responses_websocket_enabled":true,"http2_connection_shards":2}`).Error)
	_, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
	require.NoError(t, err)
	_, err = setGatewayExpression("gpt-4o-mini", `p * 2 + c * 4`)
	require.NoError(t, err)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer " + key}})
	require.NoError(t, err)
	defer client.Close()
	var upstream *websocket.Conn
	for round := 1; round <= 2; round++ {
		if round == 2 {
			_, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
			require.NoError(t, err)
			_, err = setGatewayExpression("gpt-4o-mini", `p * 4 + c * 8`)
			require.NoError(t, err)
		}
		request := fmt.Sprintf(`{"type":"response.create","stream_id":"stream%d","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`, round)
		require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(request)))
		if upstream == nil {
			select {
			case upstream = <-accepted:
				defer upstream.Close()
			case <-time.After(3 * time.Second):
				_ = client.SetReadDeadline(time.Now().Add(time.Second))
				_, payload, readErr := client.ReadMessage()
				t.Fatalf("no upstream handshake: %s %v", payload, readErr)
			}
		}
		require.NoError(t, upstream.SetReadDeadline(time.Now().Add(3*time.Second)))
		_, wire, err := upstream.ReadMessage()
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, common.Unmarshal(wire, &payload))
		assert.Equal(t, "response.create", payload["type"])
		assert.EqualValues(t, 10, payload["max_output_tokens"])
		var attempts int64
		require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&attempts).Error)
		assert.EqualValues(t, round, attempts, "generation is never sent without durable quote")
		terminal := fmt.Sprintf(`{"type":"response.completed","stream_id":"stream%d","response":{"id":"ws_resp_%d","object":"response","status":"completed","model":"provider-returned-model","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`, round, round)
		require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(terminal)))
		require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
		_, result, err := client.ReadMessage()
		require.NoError(t, err)
		assert.JSONEq(t, terminal, string(result))
	}
	assert.EqualValues(t, 1, handshakes.Load())
	// The terminal is delivered before settlement and permit release.
	awaitGatewayPermitsReleased(t)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.EqualValues(t, 42, user.UsedQuota, "each create freezes the currently published price")
	assert.Zero(t, user.ReservedQuota)
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
		assert.Equal(t, "provider-returned-model", other["response_model_name"])
		assert.Equal(t, "gpt-4o-mini", other["upstream_model_name"])
		assert.Equal(t, "gpt-4o-mini", log.ModelName)
	}
}

func TestGatewayResponsesWSSettlesObservedUsageAndRejectsOverlappingCreate(t *testing.T) {
	for _, ending := range []string{"unacknowledged", "partial", "terminal_without_usage"} {
		t.Run(ending, func(t *testing.T) {
			accepted := make(chan *websocket.Conn, 1)
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err == nil {
					accepted <- conn
				}
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Update("setting", `{"responses_websocket_enabled":true}`).Error)
			_, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
			require.NoError(t, err)
			_, err = setGatewayExpression("gpt-4o-mini", `p * 2 + c * 4`)
			require.NoError(t, err)
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer " + key}})
			require.NoError(t, err)
			defer client.Close()
			create := []byte(`{"type":"response.create","stream_id":"first","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
			require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
			var upstream *websocket.Conn
			select {
			case upstream = <-accepted:
				defer upstream.Close()
			case <-time.After(3 * time.Second):
				t.Fatal("upstream connection missing")
			}
			require.NoError(t, upstream.SetReadDeadline(time.Now().Add(3*time.Second)))
			_, _, err = upstream.ReadMessage()
			require.NoError(t, err)
			require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
			require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
			_, response, err := client.ReadMessage()
			require.NoError(t, err)
			assert.Contains(t, string(response), `"status":409`)
			if ending != "unacknowledged" {
				payload := `{"type":"response.output_text.delta","stream_id":"first","delta":"hello"}`
				if ending == "terminal_without_usage" {
					payload = `{"type":"response.completed","stream_id":"first","response":{"id":"usage-missing","status":"completed","output":[]}}`
				}
				require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(payload)))
				_, _, err = client.ReadMessage()
				require.NoError(t, err)
			}
			require.NoError(t, upstream.Close())
			_, _, _ = client.ReadMessage()
			awaitGatewayPermitsReleased(t)
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery).Error)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.Zero(t, user.ReservedQuota)
			if ending == "unacknowledged" {
				// The connection ended after send without any output.
				assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
				assert.Zero(t, user.UsedQuota)
			} else {
				// Without terminal usage the observed counters settle as an
				// estimate under "p * 2 + c * 4" (quota is half the sum): the
				// partial delta adds one output token to the 5-token prompt
				// estimate; a terminal without usage or output observed nothing.
				want := map[string]common.Quota{"partial": 7, "terminal_without_usage": 0}[ending]
				assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
				assert.Equal(t, want, user.UsedQuota)
				var usage billing.Usage
				require.NoError(t, common.UnmarshalJsonStr(string(recovery.UsageSnapshot), &usage))
				assert.False(t, usage.Confirmed())
			}
			var attempts int64
			require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&attempts).Error)
			assert.EqualValues(t, 1, attempts)
		})
	}
}

func TestGatewayResponsesWSRejectsChangedConnectionIdentity(t *testing.T) {
	for _, change := range []string{"model", "header", "credential", "endpoint"} {
		t.Run(change, func(t *testing.T) {
			accepted := make(chan *websocket.Conn, 1)
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err == nil {
					accepted <- conn
				}
			})
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Update("setting", `{"responses_websocket_enabled":true}`).Error)
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer " + key}})
			require.NoError(t, err)
			defer client.Close()
			create := []byte(`{"type":"response.create","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
			require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
			var upstream *websocket.Conn
			select {
			case upstream = <-accepted:
				defer upstream.Close()
			case <-time.After(3 * time.Second):
				t.Fatal("upstream missing")
			}
			require.NoError(t, upstream.SetReadDeadline(time.Now().Add(3*time.Second)))
			_, _, err = upstream.ReadMessage()
			require.NoError(t, err)
			require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"before_remap","status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`)))
			require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
			_, _, err = client.ReadMessage()
			require.NoError(t, err)
			var update map[string]any
			switch change {
			case "model":
				update = map[string]any{"param_override": `{"model":"changed-upstream-model"}`}
			case "header":
				update = map[string]any{"header_override": `{"X-Provider-Account":"changed-account"}`}
			case "credential":
				update = map[string]any{"key": "changed-upstream-key"}
			case "endpoint":
				update = map[string]any{"base_url": base + "/changed-upstream"}
			}
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Updates(update).Error)
			require.NoError(t, client.WriteMessage(websocket.TextMessage, create))
			_, response, err := client.ReadMessage()
			require.NoError(t, err)
			assert.Contains(t, string(response), "reconnect required")
			_, _, err = client.ReadMessage()
			require.Error(t, err, "changed connection identity must close the established session")
			awaitGatewayPermitsReleased(t)
			var attempts int64
			require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&attempts).Error)
			assert.EqualValues(t, 1, attempts)
		})
	}
}

func TestGatewayResponsesWSReauthenticatesEveryCreate(t *testing.T) {
	accepted := make(chan *websocket.Conn, 1)
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Update("setting", `{"responses_websocket_enabled":true}`).Error)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer " + key}})
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)))
	var upstream *websocket.Conn
	select {
	case upstream = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream missing")
	}
	defer upstream.Close()
	require.NoError(t, upstream.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err = upstream.ReadMessage()
	require.NoError(t, err)
	require.NoError(t, upstream.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"first-done","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`)))
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err = client.ReadMessage()
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Update("status", common.TokenStatusDisabled).Error)
	require.NoError(t, model.InvalidateUserTokensCache(1))
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-4o-mini","input":"again","max_output_tokens":10}`)))
	_, payload, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(payload), `"type":"error"`)
	awaitGatewayPermitsReleased(t)
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestGatewayResponsesWSRejectsFinalBudgetBeforeGeneration(t *testing.T) {
	previousCount := constant.CountToken
	constant.CountToken = true
	t.Cleanup(func() { constant.CountToken = previousCount })
	accepted := make(chan *websocket.Conn, 1)
	base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Updates(map[string]interface{}{"setting": `{"responses_websocket_enabled":true}`, "param_override": fmt.Sprintf(`{"input":%q}`, strings.Repeat("large input ", 256))}).Error)
	_, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
	require.NoError(t, err)
	_, err = setGatewayExpression("gpt-4o-mini", "p * 2 + c * 4")
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Update("available_quota", 100).Error)
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer " + key}})
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-4o-mini","input":"hello","max_output_tokens":1}`)))
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, result, err := client.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(result), `"type":"error"`)
	select {
	case upstream := <-accepted:
		upstream.Close()
	default:
	}
	awaitGatewayPermitsReleased(t)
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&count).Error)
	assert.Zero(t, count)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Zero(t, user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
}
