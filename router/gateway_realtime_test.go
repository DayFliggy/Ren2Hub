package router

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGatewayRealtime(t *testing.T) (string, string, <-chan *websocket.Conn, *atomic.Int32) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 8)
	sends := &atomic.Int32{}
	url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		if !websocket.IsWebSocketUpgrade(r) {
			gatewaySDKUpstream(w, r)
			return
		}
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if assert.NoError(t, err) {
			accepted <- conn
		}
	})
	return url, key, accepted, sends
}

func dialGatewayRealtime(t *testing.T, url, key string, accepted <-chan *websocket.Conn) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(url, "http")+"/v1/realtime?model=gpt-4o-mini", http.Header{"Authorization": {"Bearer " + key}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	select {
	case upstream := <-accepted:
		t.Cleanup(func() { _ = upstream.Close() })
		return client, upstream
	case <-time.After(3 * time.Second):
		_ = client.SetReadDeadline(time.Now().Add(time.Second))
		_, body, readErr := client.ReadMessage()
		t.Fatalf("upstream realtime handshake missing: %s, %v", body, readErr)
		return nil, nil
	}
}

func relayRealtimeEvent(t *testing.T, sender, recipient *websocket.Conn, event string) {
	t.Helper()
	require.NoError(t, sender.WriteMessage(websocket.TextMessage, []byte(event)))
	require.NoError(t, recipient.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, message, err := recipient.ReadMessage()
	require.NoError(t, err)
	assert.JSONEq(t, event, string(message))
}

func awaitGatewayPermitsReleased(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, key := range []string{service.UserAdmissionKey(1), service.ChannelAdmissionKey(1), service.ChannelAdmissionKey(4)} {
			state := service.ReadGatewayAdmissionRuntime(key)
			if state.InFlight != 0 || state.Queued != 0 {
				return false
			}
		}
		return true
	}, 3*time.Second, 10*time.Millisecond)
}

func TestGatewayRealtimeTerminationAndAccounting(t *testing.T) {
	for _, ending := range []string{"completed", "upstream_abrupt", "upstream_normal_mid_response", "client_abrupt", "client_invalid", "input_without_output"} {
		t.Run(ending, func(t *testing.T) {
			url, key, accepted, sends := setupGatewayRealtime(t)
			client, upstream := dialGatewayRealtime(t, url, key, accepted)
			relayRealtimeEvent(t, upstream, client, `{"type":"session.created","session":{"id":"isolated-session"}}`)
			for _, resource := range []string{service.UserAdmissionKey(1), service.ChannelAdmissionKey(1)} {
				count := service.ReadGatewayAdmissionRuntime(resource).InFlight
				assert.EqualValues(t, 1, count, "Realtime retains both slots during the connection")
			}
			hasOutput := ending != "client_invalid" && ending != "input_without_output"
			if hasOutput {
				relayRealtimeEvent(t, upstream, client, `{"type":"response.output_text.delta","response_id":"round-one","delta":"hello"}`)
			}
			switch ending {
			case "completed":
				for range 2 {
					relayRealtimeEvent(t, upstream, client, `{"type":"response.done","response":{"id":"round-one","status":"completed","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}`)
				}
				require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
			case "upstream_normal_mid_response":
				require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
			case "upstream_abrupt":
				require.NoError(t, upstream.Close())
			case "client_abrupt":
				require.NoError(t, client.Close())
			case "client_invalid":
				require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":`)))
			case "input_without_output":
				relayRealtimeEvent(t, client, upstream, `{"type":"session.update","session":{"instructions":"please answer with a short explanation"}}`)
				require.NoError(t, client.Close())
			}
			awaitGatewayPermitsReleased(t)
			assert.EqualValues(t, 1, sends.Load(), "a committed WebSocket session must never replay")
			var user model.User
			var logs []model.Log
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			if hasOutput {
				assert.Equal(t, 1, user.RequestCount)
				assert.EqualValues(t, 500, user.UsedQuota)
				require.Len(t, logs, 1)
				if ending == "completed" {
					assert.Equal(t, 10, logs[0].PromptTokens)
					assert.Equal(t, 20, logs[0].CompletionTokens)
					assert.Contains(t, logs[0].Content, "realtime usage: provider")
				} else {
					assert.Positive(t, logs[0].CompletionTokens)
					assert.Contains(t, logs[0].Content, "realtime usage: estimated")
				}
			} else {
				assert.Zero(t, user.RequestCount)
				assert.EqualValues(t, 9999500, user.Quota, "an accepted session with unknown consumption keeps its reservation")
				assert.EqualValues(t, 500, user.ReservedQuota)
			}
			var failures int64
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			if strings.HasPrefix(ending, "upstream_") {
				assert.Positive(t, failures, "upstream interruption remains a health failure after delivery")
			} else {
				assert.Zero(t, failures, "client input/cancellation must not penalize provider health")
			}
		})
	}
}

func TestGatewayRealtimeBillingRecoveryDoesNotReplay(t *testing.T) {
	url, key, accepted, sends := setupGatewayRealtime(t)
	client, upstream := dialGatewayRealtime(t, url, key, accepted)
	relayRealtimeEvent(t, upstream, client, `{"type":"response.output_text.delta","delta":"hello"}`)
	relayRealtimeEvent(t, upstream, client, `{"type":"response.done","response":{"id":"round-one","usage":{"input_tokens":10,"output_tokens":20}}}`)
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("realtime_settlement_fault", func(tx *gorm.DB) {
		if adjustment, ok := tx.Statement.Dest.(*model.BillingEvent); ok && adjustment.Operation == model.BillingRecoveryOperationSettle {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Create().Remove("realtime_settlement_fault") })
	require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	awaitGatewayPermitsReleased(t)
	assert.EqualValues(t, 1, sends.Load())
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettlementPending, recovery.Status)
	require.NoError(t, model.DB.Callback().Create().Remove("realtime_settlement_fault"))
	for range 2 {
		_, err := service.RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+1, 100)
		require.NoError(t, err)
	}
	var user model.User
	var logs int64
	require.NoError(t, model.DB.First(&user, 1).Error)
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&logs).Error)
	assert.EqualValues(t, 500, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	assert.EqualValues(t, 1, logs)
	assert.EqualValues(t, 1, sends.Load())
}
