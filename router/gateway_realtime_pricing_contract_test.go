package router

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestGatewayRealtimeFixedPriceCoversAudioAndSettlesOnce(t *testing.T) {
	for _, mode := range []string{"expression", "calls"} {
		t.Run(mode, func(t *testing.T) {
			url, key, accepted, sends := setupGatewayRealtime(t)
			if mode == "expression" {
				_, err := setGatewayExpression("gpt-4o-mini", `tier("base", fixed(0.000006))`)
				require.NoError(t, err)
			} else {
				require.NoError(t, setGatewayModelPrice("gpt-4o-mini", model.PricingValues{"ModelPrice": "0.000006"}))
			}
			client, upstream := dialGatewayRealtime(t, url, key, accepted)
			for _, id := range []string{"first", "second"} {
				relayRealtimeEvent(t, upstream, client, `{"type":"response.output_text.delta","response_id":"`+id+`","delta":"hello"}`)
				relayRealtimeEvent(t, upstream, client, `{"type":"response.done","response":{"id":"`+id+`","status":"completed","usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12,"input_token_details":{"audio_tokens":5},"output_token_details":{"audio_tokens":7}}}}`)
			}
			require.NoError(t, upstream.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
			awaitGatewayPermitsReleased(t)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			require.EqualValues(t, 3, user.UsedQuota)
			require.Zero(t, user.ReservedQuota)
			require.Equal(t, 1, user.RequestCount)
			require.EqualValues(t, 1, sends.Load())
			var events []model.BillingEvent
			require.NoError(t, model.DB.Where("operation = ?", "settle").Find(&events).Error)
			require.Len(t, events, 1)
			require.NotEmpty(t, events[0].EvidenceHash)
		})
	}
}
