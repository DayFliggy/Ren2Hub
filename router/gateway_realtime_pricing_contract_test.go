package router

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestGatewayRealtimeFixedPriceCoversAudioAndSettlesOnce(t *testing.T) {
	for _, mode := range []string{"flat_fee", "calls"} {
		t.Run(mode, func(t *testing.T) {
			url, key, accepted, sends := setupGatewayRealtime(t)
			previous, err := model.GetActiveBillingPrice(context.Background(), "gpt-4o-mini")
			require.NoError(t, err)
			version := billing.PriceVersion{Model: "gpt-4o-mini", Mode: billing.ModeUnitPrice,
				Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "fixed Realtime request price"}}
			if mode == "flat_fee" {
				version.FlatFee = "0.000006"
			} else {
				version.Prices = []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.000006"}}
			}
			_, err = model.PublishBillingPrice(context.Background(), version, previous.ID, 1)
			require.NoError(t, err)
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
