package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// A response.create with generate:false asks the upstream to take the input
// without generating (clients use it to warm a connection up), so its empty
// completion is the normal result: repeated warm-ups keep the native result
// and never open the breaker. The same empty completion of a request that asks
// for generation is still an empty stream.
func TestResponsesWebSocketWarmUpIsNotEmptyStream(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("input", p * 2)`, func(ws *websocket.Conn, _ *http.Request) {
		for turn := 0; ; turn++ {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
			for _, event := range []string{
				fmt.Sprintf(`{"type":"response.created","response":{"id":"resp_%d","status":"in_progress"}}`, turn),
				fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","output":[],"usage":{"input_tokens":5,"output_tokens":0,"total_tokens":5}}}`, turn),
			} {
				if err := ws.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
					return
				}
			}
		}
	})
	require.False(t, routeBreakerOpen(fixture.channel.Id), "the channel starts without an open breaker")
	send := func(create string) {
		t.Helper()
		require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(create)))
		require.Equal(t, "response.created", readResponsesWSTestEvent(t, fixture.client)["type"])
		require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
	}
	// One more than the five consecutive failures that open the breaker.
	const warmUps = 6
	for range warmUps {
		send(`{"type":"response.create","model":"ws-billing","input":"hi","generate":false}`)
	}
	require.False(t, routeBreakerOpen(fixture.channel.Id), "warm-ups never open the breaker")
	send(`{"type":"response.create","model":"ws-billing","input":"hi"}`)
	fixture.closeAndWait(t)

	outcomes := responsesWSRouteOutcomes(t, fixture)
	require.Len(t, outcomes, warmUps+1)
	for _, outcome := range outcomes[:warmUps] {
		require.Equal(t, [2]string{"success", ""}, outcome, "a warm-up keeps the native result")
	}
	require.Equal(t, [2]string{"failure", "empty_stream"}, outcomes[warmUps], "an empty completion of a generating request is an empty stream")
}

func routeBreakerOpen(channelID int) bool {
	for _, row := range service.RoutingSnapshot().Breakers {
		if row.ChannelID == channelID {
			return true
		}
	}
	return false
}

// responsesWSRouteOutcomes returns the outcome and reason of the route attempt
// in each consume log of the fixture's token, oldest first.
func responsesWSRouteOutcomes(t *testing.T, fixture *responsesWSBillingTest) [][2]string {
	t.Helper()
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("token_id = ? AND type = ?", fixture.token.Id, model.LogTypeConsume).Order("id").Find(&logs).Error)
	outcomes := make([][2]string, 0, len(logs))
	for _, log := range logs {
		var other struct {
			AdminInfo struct {
				RequestPolicy []struct {
					RouteAttempt *struct {
						Outcome       string `json:"outcome"`
						OutcomeReason string `json:"outcome_reason"`
					} `json:"route_attempt"`
				} `json:"request_policy"`
			} `json:"admin_info"`
		}
		require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
		for _, event := range other.AdminInfo.RequestPolicy {
			if event.RouteAttempt != nil {
				outcomes = append(outcomes, [2]string{event.RouteAttempt.Outcome, event.RouteAttempt.OutcomeReason})
			}
		}
	}
	return outcomes
}
