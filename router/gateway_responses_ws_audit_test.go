package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupResponsesWSAudit(t *testing.T) (string, string, string, <-chan *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 4)
	base, key, foreign := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			accepted <- conn
		}
	})
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Update("setting", `{"responses_websocket_enabled":true}`).Error)
	return base, key, foreign, accepted
}

func dialResponsesWSAudit(t *testing.T, base, key string) *websocket.Conn {
	t.Helper()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", http.Header{"Authorization": {"Bearer " + key}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func acceptResponsesWSAudit(t *testing.T, accepted <-chan *websocket.Conn) *websocket.Conn {
	t.Helper()
	select {
	case conn := <-accepted:
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	case <-time.After(3 * time.Second):
		t.Fatal("upstream websocket was not opened")
		return nil
	}
}

func readResponsesWSAudit(t *testing.T, connection *websocket.Conn) string {
	t.Helper()
	require.NoError(t, connection.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, body, err := connection.ReadMessage()
	require.NoError(t, err)
	return string(body)
}

func writeResponsesWSAudit(t *testing.T, connection *websocket.Conn, body string) {
	t.Helper()
	require.NoError(t, connection.WriteMessage(websocket.TextMessage, []byte(body)))
}

func completeResponsesWSAudit(t *testing.T, upstream, client *websocket.Conn, id string) {
	t.Helper()
	body := fmt.Sprintf(`{"type":"response.completed","stream_id":"active","response":{"id":%q,"object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`, id)
	writeResponsesWSAudit(t, upstream, body)
	assert.JSONEq(t, body, readResponsesWSAudit(t, client))
}

// Every call ends without holding funds: a settled call charges the fixture's
// fixed 500-quota price, a released call charges nothing.
func assertResponsesWSAuditAccounting(t *testing.T, status string, attempts int64) {
	t.Helper()
	awaitGatewayPermitsReleased(t)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.Order("created_at desc").First(&recovery).Error)
	assert.Equal(t, status, recovery.Status)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Zero(t, user.ReservedQuota)
	if status == model.BillingRecoveryStatusSettled {
		assert.EqualValues(t, 500, user.UsedQuota)
	} else {
		assert.Zero(t, user.UsedQuota)
	}
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&count).Error)
	assert.Equal(t, attempts, count)
}

func TestGatewayResponsesWSSubprotocolAuthentication(t *testing.T) {
	for _, mode := range []string{"bearer", "browser_key", "unrelated_key_prefix"} {
		t.Run(mode, func(t *testing.T) {
			base, key, _, accepted := setupResponsesWSAudit(t)
			dialer := *websocket.DefaultDialer
			dialer.Subprotocols = []string{"responses"}
			headers := http.Header{"Authorization": {"Bearer " + key}}
			switch mode {
			case "browser_key":
				headers.Del("Authorization")
				dialer.Subprotocols = append(dialer.Subprotocols, "openai-insecure-api-key."+key)
			case "unrelated_key_prefix":
				dialer.Subprotocols = append(dialer.Subprotocols, "openai-insecure-api-key-disabled")
			}
			client, response, err := dialer.Dial("ws"+strings.TrimPrefix(base, "http")+"/v1/responses", headers)
			if response != nil {
				t.Logf("handshake HTTP status: %d", response.StatusCode)
			}
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			assert.Equal(t, "responses", client.Subprotocol())
			var upstream *websocket.Conn
			for _, id := range []string{"first-subprotocol-response", "second-subprotocol-response"} {
				writeResponsesWSAudit(t, client, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
				if upstream == nil {
					upstream = acceptResponsesWSAudit(t, accepted)
				}
				readResponsesWSAudit(t, upstream)
				completeResponsesWSAudit(t, upstream, client, id)
			}
			assert.Empty(t, accepted)
			awaitGatewayPermitsReleased(t)
			var attempts int64
			require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&attempts).Error)
			assert.EqualValues(t, 2, attempts, "captured credentials authorize each generation")
		})
	}
}

func TestGatewayResponsesWSPreviousResponseOwnership(t *testing.T) {
	base, key, foreignKey, accepted := setupResponsesWSAudit(t)
	client := dialResponsesWSAudit(t, base, key)
	writeResponsesWSAudit(t, client, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
	upstream := acceptResponsesWSAudit(t, accepted)
	readResponsesWSAudit(t, upstream)
	completeResponsesWSAudit(t, upstream, client, "owned-ws-response")
	continuation := `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","previous_response_id":"owned-ws-response","input":"continue","max_output_tokens":10}`
	writeResponsesWSAudit(t, client, continuation)
	assert.Contains(t, readResponsesWSAudit(t, upstream), `"previous_response_id":"owned-ws-response"`)
	completeResponsesWSAudit(t, upstream, client, "continued-ws-response")
	foreign := dialResponsesWSAudit(t, base, foreignKey)
	writeResponsesWSAudit(t, foreign, continuation)
	assert.Contains(t, readResponsesWSAudit(t, foreign), `"status":400`)
	writeResponsesWSAudit(t, client, strings.ReplaceAll(continuation, "owned-ws-response", "unknown-ws-response"))
	assert.Contains(t, readResponsesWSAudit(t, client), `"status":400`)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("param_override", `{"model":"other-upstream-model"}`).Error)
	writeResponsesWSAudit(t, client, continuation)
	assert.Contains(t, readResponsesWSAudit(t, client), "reconnect required")
	reconnected := dialResponsesWSAudit(t, base, key)
	writeResponsesWSAudit(t, reconnected, continuation)
	assert.Contains(t, readResponsesWSAudit(t, reconnected), `"type":"error"`, "reconnecting must not move an old native response to another resolved model")
	awaitGatewayPermitsReleased(t)
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&count).Error)
	assert.EqualValues(t, 2, count, "foreign, missing and remapped native state never reaches generation")
	assert.Empty(t, accepted, "state rejection must not open an unrelated upstream connection")
}

func TestGatewayResponsesWSReconnectPreservesOnlySameOwner(t *testing.T) {
	for _, change := range []string{"none", "model", "credential", "header", "endpoint"} {
		t.Run(change, func(t *testing.T) {
			base, key, _, accepted := setupResponsesWSAudit(t)
			client := dialResponsesWSAudit(t, base, key)
			writeResponsesWSAudit(t, client, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
			upstream := acceptResponsesWSAudit(t, accepted)
			readResponsesWSAudit(t, upstream)
			completeResponsesWSAudit(t, upstream, client, "before-reconnect")
			require.NoError(t, client.Close())
			var update map[string]any
			switch change {
			case "model":
				update = map[string]any{"param_override": `{"model":"changed-upstream-model"}`}
			case "credential":
				update = map[string]any{"key": "changed-upstream-key"}
			case "header":
				update = map[string]any{"header_override": `{"X-Provider-Account":"changed-account"}`}
			case "endpoint":
				update = map[string]any{"base_url": base + "/changed-upstream"}
			}
			if update != nil {
				require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Updates(update).Error)
			}
			reconnected := dialResponsesWSAudit(t, base, key)
			writeResponsesWSAudit(t, reconnected, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","previous_response_id":"before-reconnect","input":"continue","max_output_tokens":10}`)
			wantAttempts := int64(1)
			if change == "none" {
				continued := acceptResponsesWSAudit(t, accepted)
				assert.Contains(t, readResponsesWSAudit(t, continued), `"previous_response_id":"before-reconnect"`)
				completeResponsesWSAudit(t, continued, reconnected, "after-reconnect")
				wantAttempts = 2
			} else {
				assert.Contains(t, readResponsesWSAudit(t, reconnected), `"type":"error"`)
				assert.Empty(t, accepted, "ownership mismatch must fail before an upstream handshake")
			}
			awaitGatewayPermitsReleased(t)
			var attempts, recoveries int64
			require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&attempts).Error)
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
			assert.Equal(t, wantAttempts, attempts)
			assert.Equal(t, wantAttempts, recoveries, "ownership mismatch must not create a quotation or reservation")
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.Zero(t, user.ReservedQuota)
			assert.EqualValues(t, wantAttempts*500, user.UsedQuota)
		})
	}
}

func TestGatewayResponsesWSCancelErrorKeepsActiveGeneration(t *testing.T) {
	base, key, _, accepted := setupResponsesWSAudit(t)
	client := dialResponsesWSAudit(t, base, key)
	writeResponsesWSAudit(t, client, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
	upstream := acceptResponsesWSAudit(t, accepted)
	readResponsesWSAudit(t, upstream)
	created := `{"type":"response.created","stream_id":"active","response":{"id":"cancel-active","status":"in_progress"}}`
	writeResponsesWSAudit(t, upstream, created)
	assert.JSONEq(t, created, readResponsesWSAudit(t, client))
	writeResponsesWSAudit(t, client, `{"type":"response.cancel","event_id":"wrong-stream","stream_id":"other"}`)
	localError := readResponsesWSAudit(t, client)
	assert.Contains(t, localError, `"event_id":"wrong-stream"`)
	assert.Contains(t, localError, "cancellation stream_id does not match")
	cancel := `{"type":"response.cancel","event_id":"cancel-one","stream_id":"active","response_id":"wrong-response"}`
	writeResponsesWSAudit(t, client, cancel)
	assert.JSONEq(t, cancel, readResponsesWSAudit(t, upstream))
	rejection := `{"type":"error","status":400,"event_id":"cancel-one","stream_id":"active","error":{"type":"invalid_request_error","code":"response_not_found","message":"unknown cancellation target"}}`
	writeResponsesWSAudit(t, upstream, rejection)
	assert.JSONEq(t, rejection, readResponsesWSAudit(t, client))
	var before model.User
	require.NoError(t, model.DB.First(&before, 1).Error)
	assert.Positive(t, before.ReservedQuota, "a rejected cancellation cannot refund an active generation")
	completeResponsesWSAudit(t, upstream, client, "cancel-active")
	assertResponsesWSAuditAccounting(t, model.BillingRecoveryStatusSettled, 1)
}

func TestGatewayResponsesWSRejectionHonorsSharedRetryBudget(t *testing.T) {
	for _, code := range []int{400, 429, 408, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			base, key, _, accepted := setupResponsesWSAudit(t)
			client := dialResponsesWSAudit(t, base, key)
			writeResponsesWSAudit(t, client, `{"type":"response.create","event_id":"create-one","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
			upstream := acceptResponsesWSAudit(t, accepted)
			readResponsesWSAudit(t, upstream)
			rejection := fmt.Sprintf(`{"type":"error","status":%d,"event_id":"create-one","stream_id":"active","error":{"type":"upstream_error","code":"fixture_rejected","message":"fixture rejected generation"}}`, code)
			writeResponsesWSAudit(t, upstream, rejection)
			assert.JSONEq(t, rejection, readResponsesWSAudit(t, client))
			// A rejection without output releases funds, including 408 and 5xx.
			assertResponsesWSAuditAccounting(t, model.BillingRecoveryStatusRefunded, 1)
			assert.Equal(t, 0, service.DefaultSameKeyAttempts)
			assert.Empty(t, accepted, "the locked WS credential must not bypass the shared zero same-key retry budget or change its connection owner")
		})
	}
}

func TestGatewayResponsesWSClientDisconnectSettlesDeliveredOutput(t *testing.T) {
	base, key, _, accepted := setupResponsesWSAudit(t)
	client := dialResponsesWSAudit(t, base, key)
	writeResponsesWSAudit(t, client, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
	upstream := acceptResponsesWSAudit(t, accepted)
	readResponsesWSAudit(t, upstream)
	payload := `{"type":"response.output_text.delta","stream_id":"active","delta":"` + strings.Repeat("x ", 128<<10) + `"}`
	done := make(chan error, 1)
	go func() { done <- upstream.WriteMessage(websocket.TextMessage, []byte(payload)) }()
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, reader, err := client.NextReader()
	require.NoError(t, err)
	_, err = io.ReadFull(reader, make([]byte, 32))
	require.NoError(t, err)
	require.NoError(t, client.UnderlyingConn().Close())
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream write did not unblock after client disconnect")
	}
	// Output reached the client before the disconnect; the fixed-price call
	// has no terminal event and settles at its frozen estimate.
	assertResponsesWSAuditAccounting(t, model.BillingRecoveryStatusSettled, 1)
}

func TestGatewayResponsesWSIncompleteClientFrameNeverSends(t *testing.T) {
	base, key, _, accepted := setupResponsesWSAudit(t)
	client := dialResponsesWSAudit(t, base, key)
	writer, err := client.NextWriter(websocket.TextMessage)
	require.NoError(t, err)
	_, err = writer.Write([]byte(`{"type":"response.create","model":"gpt-4o-mini","input":"` + strings.Repeat("x", 64<<10)))
	require.NoError(t, err)
	require.NoError(t, client.UnderlyingConn().Close())
	awaitGatewayPermitsReleased(t)
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&count).Error)
	assert.Zero(t, count)
	assert.Empty(t, accepted)
}

func TestGatewayResponsesWSDrainLifecycle(t *testing.T) {
	scenario := os.Getenv("REN2HUB_WS_DRAIN_TEST")
	if scenario == "" {
		for _, value := range []string{"completed", "deadline"} {
			t.Run(value, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGatewayResponsesWSDrainLifecycle$", "-test.timeout=18s")
				command.Env = append(os.Environ(), "REN2HUB_WS_DRAIN_TEST="+value)
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
			})
		}
		return
	}
	base, key, _, accepted := setupResponsesWSAudit(t)
	client := dialResponsesWSAudit(t, base, key)
	idle := dialResponsesWSAudit(t, base, key)
	writeResponsesWSAudit(t, client, `{"type":"response.create","stream_id":"active","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
	upstream := acceptResponsesWSAudit(t, accepted)
	readResponsesWSAudit(t, upstream)
	service.BeginGatewayDrain()
	writeResponsesWSAudit(t, idle, `{"type":"response.create","model":"gpt-4o-mini","input":"after drain"}`)
	assert.Contains(t, readResponsesWSAudit(t, idle), `"status":503`)
	_ = idle.Close()
	drainCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- service.WaitGatewayDrain(drainCtx) }()
	if scenario == "completed" {
		completeResponsesWSAudit(t, upstream, client, "drained-success")
		_ = client.Close()
		require.NoError(t, <-done)
		assertResponsesWSAuditAccounting(t, model.BillingRecoveryStatusSettled, 1)
		return
	}
	require.ErrorIs(t, <-done, context.DeadlineExceeded)
	require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := client.ReadMessage()
	require.Error(t, err)
	// The drain deadline closed a call that produced no output.
	assertResponsesWSAuditAccounting(t, model.BillingRecoveryStatusRefunded, 1)
}
