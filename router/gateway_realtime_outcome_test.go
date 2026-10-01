package router

import (
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayRealtimeResponseOutcomes(t *testing.T) {
	for _, test := range []struct {
		name, event         string
		output, fault, stop bool
	}{
		{"failed_response", `{"type":"response.done","response":{"id":"response-1","status":"failed","status_details":{"error":{"type":"server_error","code":"server_error"}},"usage":{"input_tokens":10,"output_tokens":20}}}`, true, true, true},
		{"provider_rate_limit", `{"type":"error","error":{"type":"rate_limit_error","code":"rate_limit_exceeded","message":"rate limit"}}`, false, true, true},
		{"provider_invalid_input", `{"type":"error","error":{"type":"invalid_request_error","message":"invalid event"}}`, false, false, true},
		{"cancelled_response", `{"type":"response.done","response":{"id":"response-1","status":"cancelled","status_details":{"reason":"client_cancelled"},"usage":{"input_tokens":10,"output_tokens":20}}}`, true, false, false},
		{"limited_response", `{"type":"response.done","response":{"id":"response-1","status":"incomplete","status_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":10,"output_tokens":20}}}`, true, false, false},
		{"usage_without_output", `{"type":"response.done","response":{"id":"response-1","status":"completed","usage":{"input_tokens":10,"output_tokens":20}}}`, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			url, key, accepted, sends := setupGatewayRealtime(t)
			client, upstream := dialGatewayRealtime(t, url, key, accepted)
			if test.output {
				relayRealtimeEvent(t, upstream, client, `{"type":"response.output_text.delta","delta":"hello"}`)
			}
			relayRealtimeEvent(t, upstream, client, test.event)
			if !test.stop {
				require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
			}
			awaitGatewayPermitsReleased(t)
			// Channel permit release precedes request-final settlement by a small interval.
			var row model.BillingRecovery
			require.Eventually(t, func() bool {
				return model.DB.First(&row).Error == nil && (row.Status == model.BillingRecoveryStatusRefunded || row.Status == model.BillingRecoveryStatusSettled)
			}, 2*time.Second, 10*time.Millisecond)
			// Delivered output or completed usage bills the fixed 500-quota call;
			// a provider error before any output releases the reservation.
			wantQuota := common.Quota(0)
			wantStatus := model.BillingRecoveryStatusRefunded
			if test.output || test.name == "usage_without_output" {
				wantQuota, wantStatus = 500, model.BillingRecoveryStatusSettled
			}
			assert.Equal(t, wantStatus, row.Status)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.Equal(t, wantQuota, user.UsedQuota)
			assert.Equal(t, 10000000-wantQuota, user.Quota)
			assert.Zero(t, user.ReservedQuota)
			var failures int64
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			assert.Equal(t, test.fault, failures > 0, fmt.Sprint(test.name))
			assert.EqualValues(t, 1, sends.Load())
		})
	}
}
