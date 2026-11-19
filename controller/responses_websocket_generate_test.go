package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// warmUpUpstream answers each response.create on the upstream connection: a
// create whose received generate is false gets an empty completion, any other
// create gets a completion with text when withText is set and an empty one
// otherwise. Each received generate value is sent to seen when it is set.
func warmUpUpstream(t *testing.T, withText bool, seen chan<- any) func(*websocket.Conn, *http.Request) {
	return func(ws *websocket.Conn, _ *http.Request) {
		for turn := 0; ; turn++ {
			_, raw, err := ws.ReadMessage()
			if err != nil {
				return
			}
			var request map[string]any
			if err := common.Unmarshal(raw, &request); err != nil {
				t.Errorf("upstream request is not JSON: %v", err)
				return
			}
			if seen != nil {
				seen <- request["generate"]
			}
			output, outputTokens := `[]`, 0
			if request["generate"] != false && withText {
				output, outputTokens = `[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]`, 1
			}
			for _, event := range []string{
				fmt.Sprintf(`{"type":"response.created","response":{"id":"resp_%d","status":"in_progress"}}`, turn),
				fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_%d","status":"completed","output":%s,"usage":{"input_tokens":5,"output_tokens":%d,"total_tokens":%d}}}`, turn, output, outputTokens, 5+outputTokens),
			} {
				if err := ws.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
					return
				}
			}
		}
	}
}

func sendResponsesWSCreate(t *testing.T, client *websocket.Conn, create string) {
	t.Helper()
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(create)))
	require.Equal(t, "response.created", readResponsesWSTestEvent(t, client)["type"])
	require.Equal(t, "response.completed", readResponsesWSTestEvent(t, client)["type"])
}

// A response.create with generate:false asks the upstream to take the input
// without generating (clients use it to warm a connection up). Its empty
// completion is neither an empty stream nor a generation success: routing
// ignores it (no_generation), so repeated warm-ups never open the breaker. The
// same empty completion of a request that asks for generation is still an
// empty stream.
func TestResponsesWebSocketWarmUpIsNotEmptyStream(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("input", p * 2)`, warmUpUpstream(t, false, nil))
	require.False(t, routeBreakerOpen(fixture.channel.Id), "the channel starts without an open breaker")
	// One more than the five consecutive failures that open the breaker.
	const warmUps = 6
	for range warmUps {
		sendResponsesWSCreate(t, fixture.client, `{"type":"response.create","model":"ws-billing","input":"hi","generate":false}`)
	}
	require.False(t, routeBreakerOpen(fixture.channel.Id), "warm-ups never open the breaker")
	sendResponsesWSCreate(t, fixture.client, `{"type":"response.create","model":"ws-billing","input":"hi"}`)
	fixture.closeAndWait(t)

	outcomes := responsesWSRouteOutcomes(t, fixture)
	require.Len(t, outcomes, warmUps+1)
	for _, outcome := range outcomes[:warmUps] {
		require.Equal(t, [2]string{"ignored", "no_generation"}, outcome, "a warm-up is not a generation result")
	}
	require.Equal(t, [2]string{"failure", "empty_stream"}, outcomes[warmUps], "an empty completion of a generating request is an empty stream")
}

// The warm-up judgment reads the generate the upstream actually receives. A
// channel parameter override can turn a create that omits generate into a
// warm-up; an explicit generate in the create still wins over the override.
func TestResponsesWebSocketWarmUpFollowsTheSentGenerate(t *testing.T) {
	seen := make(chan any, 4)
	fixture := newResponsesWSBillingTestWithSetup(t, `tier("input", p * 2)`, warmUpUpstream(t, false, seen), func(f *responsesWSBillingTest) string {
		require.NoError(t, model.DB.Model(f.channel).Update("param_override", `{"generate":false}`).Error)
		return "Bearer sk-" + f.token.Key
	})
	sendResponsesWSCreate(t, fixture.client, `{"type":"response.create","model":"ws-billing","input":"hi"}`)
	require.Equal(t, false, <-seen, "the override makes the upstream request a warm-up")
	sendResponsesWSCreate(t, fixture.client, `{"type":"response.create","model":"ws-billing","input":"hi","generate":true}`)
	require.Equal(t, true, <-seen, "an explicit generate wins over the override")
	fixture.closeAndWait(t)

	require.Equal(t, [][2]string{{"ignored", "no_generation"}, {"failure", "empty_stream"}}, responsesWSRouteOutcomes(t, fixture))
}

// Warm-ups never decide a half-open breaker: three warm-ups on independent
// connections, each able to take the probe permit, leave it half-open with the
// permit free and add no quality or observation sample. Probes that generate
// then close it.
func TestResponsesWebSocketWarmUpDoesNotCloseHalfOpenBreaker(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("input", p * 2)`, warmUpUpstream(t, true, nil))
	const actualModel = "gpt-4o"
	// Open the breaker in virtual time far enough in the past that it is half
	// open for the real requests below.
	synctest.Test(t, func(t *testing.T) {
		for range 5 {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			common.SetContextKey(c, constant.ContextKeyChannelId, fixture.channel.Id)
			c.Set("model_mapping", `{"ws-billing":"`+actualModel+`"}`)
			service.BeginRouteObservation(c)
			info := &relaycommon.RelayInfo{OriginModelName: "ws-billing"}
			service.MarkRouteAttemptSent(c, info)
			service.FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("fixture failure"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))
		}
	})
	t.Cleanup(func() {
		require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), fixture.channel.Id))
	})
	breakerState := func() string {
		t.Helper()
		breakers := service.RoutingSnapshot().Breakers
		index := slices.IndexFunc(breakers, func(b service.RouteBreakerSnapshot) bool {
			return b.ChannelID == fixture.channel.Id && b.Model == actualModel
		})
		if index < 0 {
			return "closed"
		}
		require.False(t, breakers[index].ProbeInFlight, "the probe permit is free again")
		return breakers[index].State
	}
	samples := func() int {
		snapshot := service.RoutingSnapshot()
		total := 0
		for _, row := range snapshot.Observations {
			if row.ChannelID == fixture.channel.Id {
				total += row.Samples
			}
		}
		for _, row := range snapshot.Quality {
			if row.ChannelID == fixture.channel.Id {
				total += row.Samples
			}
		}
		return total
	}
	require.Equal(t, "half_open", breakerState())
	before := samples()

	gateway := newResponsesWSGateway(t)
	connect := func(create string) {
		t.Helper()
		client, _, err := websocket.DefaultDialer.Dial(gateway.url, http.Header{"Authorization": []string{"Bearer sk-" + fixture.token.Key}})
		require.NoError(t, err)
		sendResponsesWSCreate(t, client, create)
		require.NoError(t, client.Close())
		<-gateway.ended
	}
	for range 3 {
		connect(`{"type":"response.create","model":"ws-billing","input":"hi","generate":false}`)
		require.Equal(t, "half_open", breakerState(), "a warm-up does not count toward recovery")
	}
	require.Equal(t, before, samples(), "warm-ups add no quality or observation sample")

	for range 3 {
		connect(`{"type":"response.create","model":"ws-billing","input":"hi"}`)
	}
	require.Equal(t, "closed", breakerState(), "three generating probes close the breaker")
	require.Greater(t, samples(), before, "generating requests add samples")
}

type responsesWSGateway struct {
	url   string
	ended chan struct{}
}

// newResponsesWSGateway serves the Responses WebSocket on its own engine so a
// test can open several independent client connections; ended receives one
// value per finished connection.
func newResponsesWSGateway(t *testing.T) *responsesWSGateway {
	t.Helper()
	gateway := &responsesWSGateway{ended: make(chan struct{}, 8)}
	engine := gin.New()
	engine.GET("/v1/responses", middleware.TokenAuth(), func(c *gin.Context) {
		defer func() { gateway.ended <- struct{}{} }()
		c.Set(common.RequestIdKey, "responses-ws-warmup")
		ResponsesWebSocket(c)
	})
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	gateway.url = "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/responses"
	return gateway
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
