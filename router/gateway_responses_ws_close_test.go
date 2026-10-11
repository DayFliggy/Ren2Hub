package router

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every way a session ends sends the client a close frame whose code says
// why. A dropped socket would read as a synthetic 1006 close error, so a
// received 1000, 1001, 1008 or 1011 proves the gateway sent the frame.
func TestGatewayResponsesWSCloseCodes(t *testing.T) {
	const (
		created   = `{"type":"response.created","response":{"id":"active","status":"in_progress"}}`
		delta     = `{"type":"response.output_text.delta","delta":"hello"}`
		completed = `{"type":"response.completed","response":{"id":"active","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`
	)
	for _, tc := range []struct {
		name   string
		events []string // upstream events after the create, all relayed
		// After the events, and after the client read them, upstream sends
		// upstreamClose or drops the socket. Otherwise end runs.
		upstreamClose int
		drop          bool
		timeout       bool
		end           func(*testing.T, *websocket.Conn)
		code          int
		reason        string
		upstreamCode  int // the close code upstream receives, when checked
		accounting    string
	}{
		{name: "idle upstream closes normally", events: []string{completed}, upstreamClose: websocket.CloseNormalClosure, code: websocket.CloseNormalClosure, reason: "upstream connection closed", accounting: model.BillingRecoveryStatusSettled},
		{name: "idle upstream goes away", events: []string{completed}, upstreamClose: websocket.CloseGoingAway, code: websocket.CloseGoingAway, reason: "upstream connection closed", accounting: model.BillingRecoveryStatusSettled},
		{name: "idle upstream drops", events: []string{completed}, drop: true, code: websocket.CloseInternalServerErr, reason: "upstream connection closed", accounting: model.BillingRecoveryStatusSettled},
		// A normal upstream close still truncates the active response.
		{name: "upstream closes mid-response", events: []string{created, delta}, upstreamClose: websocket.CloseNormalClosure, code: websocket.CloseInternalServerErr, reason: "upstream connection closed", accounting: model.BillingRecoveryStatusSettled},
		{name: "upstream drops mid-response", events: []string{created, delta}, drop: true, code: websocket.CloseInternalServerErr, reason: "upstream connection closed", accounting: model.BillingRecoveryStatusSettled},
		{name: "upstream response times out", events: []string{created}, timeout: true, code: websocket.CloseInternalServerErr, reason: "upstream response timed out", upstreamCode: websocket.CloseInternalServerErr, accounting: model.BillingRecoveryStatusRefunded},
		{name: "client closes", events: []string{completed}, end: func(t *testing.T, client *websocket.Conn) {
			require.NoError(t, client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
		}, code: websocket.CloseNormalClosure, upstreamCode: websocket.CloseNormalClosure, accounting: model.BillingRecoveryStatusSettled},
		{name: "token revoked", events: []string{completed}, end: func(t *testing.T, client *websocket.Conn) {
			require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Update("status", common.TokenStatusDisabled).Error)
			require.NoError(t, model.InvalidateUserTokensCache(1))
			writeResponsesWSAudit(t, client, `{"type":"response.create","model":"gpt-4o-mini","input":"again","max_output_tokens":10}`)
			assert.Contains(t, readResponsesWSAudit(t, client), `"status":401`)
		}, code: websocket.ClosePolicyViolation, upstreamCode: websocket.ClosePolicyViolation, accounting: model.BillingRecoveryStatusSettled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.timeout {
				previous := constant.StreamingTimeout
				constant.StreamingTimeout = 1
				t.Cleanup(func() { constant.StreamingTimeout = previous })
			}
			base, key, _, accepted := setupResponsesWSAudit(t)
			client := dialResponsesWSAudit(t, base, key)
			writeResponsesWSAudit(t, client, `{"type":"response.create","model":"gpt-4o-mini","input":"hello","max_output_tokens":10}`)
			upstream := acceptResponsesWSAudit(t, accepted)
			readResponsesWSAudit(t, upstream)
			for _, event := range tc.events {
				writeResponsesWSAudit(t, upstream, event)
				assert.JSONEq(t, event, readResponsesWSAudit(t, client))
			}
			switch {
			case tc.drop:
				require.NoError(t, upstream.UnderlyingConn().Close())
			case tc.upstreamClose != 0:
				require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(tc.upstreamClose, "upstream shutdown"), time.Now().Add(time.Second)))
			case tc.end != nil:
				tc.end(t, client)
			}

			// A request cut off mid-response reports its error event first.
			require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
			var err error
			for {
				var body []byte
				if _, body, err = client.ReadMessage(); err != nil {
					break
				}
				assert.Contains(t, string(body), `"type":"error"`)
			}
			var closeErr *websocket.CloseError
			require.ErrorAs(t, err, &closeErr)
			assert.Equal(t, tc.code, closeErr.Code)
			assert.Equal(t, tc.reason, closeErr.Text)
			if tc.upstreamCode != 0 {
				require.NoError(t, upstream.SetReadDeadline(time.Now().Add(5*time.Second)))
				for {
					if _, _, err = upstream.ReadMessage(); err != nil {
						break
					}
				}
				require.ErrorAs(t, err, &closeErr)
				assert.Equal(t, tc.upstreamCode, closeErr.Code)
			}
			assertResponsesWSAuditAccounting(t, tc.accounting, 1)
		})
	}
}
