package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestResponsesWebSocketPermitsOnlyActiveCreateAndRejectsCompetingRequest(t *testing.T) {
	for _, resource := range []string{"user", "channel"} {
		t.Run(resource, func(t *testing.T) {
			started := make(chan struct{}, 1)
			finish := make(chan struct{})
			var finishOnce sync.Once
			unblock := func() { finishOnce.Do(func() { close(finish) }) }
			defer unblock()
			fixture := newResponsesWSBillingTestWithSetup(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
				if _, _, err := ws.ReadMessage(); err != nil {
					return
				}
				started <- struct{}{}
				<-finish
				_ = ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"capacity","status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`))
				_, _, _ = ws.ReadMessage()
			}, func(f *responsesWSBillingTest) string {
				userLimit, channelLimit := 0, 0
				if resource == "user" {
					userLimit = 1
				} else {
					channelLimit = 1
				}
				require.NoError(t, model.DB.Model(f.user).Update("max_concurrency", userLimit).Error)
				require.NoError(t, model.DB.Model(f.channel).Update("max_concurrency", channelLimit).Error)
				require.NoError(t, service.RefreshUserConcurrencyLimit(context.Background(), f.user.Id))
				require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), f.channel.Id))
				t.Cleanup(func() {
					require.NoError(t, model.DB.Model(f.channel).Update("max_concurrency", 0).Error)
					require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), f.channel.Id))
				})
				return "Bearer sk-" + f.token.Key
			})
			occupancy := func() (int, int) {
				userActive := 0
				for _, row := range service.RoutingSnapshot().Users {
					if row.ID == fixture.user.Id {
						userActive = row.InFlight
					}
				}
				return userActive, service.ChannelInFlight(fixture.channel.Id)
			}
			u, ch := occupancy()
			require.Zero(t, u)
			require.Zero(t, ch)
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
			select {
			case <-started:
			case <-time.After(3 * time.Second):
				t.Fatal("upstream did not receive create")
			}
			u, ch = occupancy()
			require.Equal(t, 1, u)
			require.Equal(t, 1, ch)
			request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"competing","stream":true}`))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			body, _ := io.ReadAll(response.Body)
			require.NoError(t, response.Body.Close())
			require.Equal(t, http.StatusTooManyRequests, response.StatusCode, fmt.Sprintf("%s", body))
			unblock()
			require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
			require.Eventually(t, func() bool { u, ch := occupancy(); return u == 0 && ch == 0 }, 3*time.Second, 10*time.Millisecond)
			require.EqualValues(t, 1, fixture.connections.Load())
			fixture.closeAndWait(t)
		})
	}
}

func TestHTTPRetryReservesExpensiveChannelBeforeDispatch(t *testing.T) {
	for _, wallet := range []int{100000, 2000} {
		t.Run(fmt.Sprint(wallet), func(t *testing.T) {
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
			oldRetries := common.RetryTimes
			common.RetryTimes = 1
			t.Cleanup(func() { common.RetryTimes = oldRetries })
			require.NoError(t, model.DecreaseUserQuota(fixture.user.Id, 100000-wallet, false))
			require.NoError(t, model.DB.Model(fixture.token).Update("remain_quota", 10000).Error)
			var firstCalls, secondCalls atomic.Int32
			reservations := make(chan int, 2)
			capture := func() {
				token, err := model.GetTokenById(fixture.token.Id)
				if err != nil {
					t.Error(err)
					return
				}
				reservations <- token.UsedQuota
			}
			fixture.httpUpstream = func(w http.ResponseWriter, r *http.Request) {
				firstCalls.Add(1)
				capture()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = io.WriteString(w, `{"error":{"message":"retry","type":"server_error"}}`)
			}
			second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				secondCalls.Add(1)
				capture()
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"retry_ok","object":"response","model":"ws-billing","status":"completed","output":[],"usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}`)
			}))
			defer second.Close()
			ch := &model.Channel{Name: "expensive-retry", Key: "test-upstream", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "ws-billing", BaseURL: &second.URL, ChannelRatio: "3"}
			require.NoError(t, model.DB.Create(ch).Error)
			require.NoError(t, ch.AddAbilities(nil))
			_, err := model.SaveTokenRouting(context.Background(), fixture.user.Id, fixture.token.Id, model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual}, Entries: []model.TokenRouteEntry{{ChannelID: fixture.channel.Id, Enabled: true}, {ChannelID: ch.Id, Enabled: true}}}, map[int]bool{fixture.channel.Id: true, ch.Id: true})
			require.NoError(t, err)
			req, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"retry"}`))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
			response, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			body, _ := io.ReadAll(response.Body)
			require.NoError(t, response.Body.Close())
			require.EqualValues(t, 1, firstCalls.Load())
			require.Equal(t, 1000, <-reservations)
			if wallet == 2000 {
				require.Equal(t, http.StatusForbidden, response.StatusCode, string(body))
				require.Zero(t, secondCalls.Load())
				require.Eventually(t, func() bool {
					user, _ := model.GetUserById(fixture.user.Id, false)
					token, _ := model.GetTokenById(fixture.token.Id)
					return user != nil && token != nil && user.Quota == wallet && token.RemainQuota == 10000
				}, 3*time.Second, 10*time.Millisecond)
			} else {
				require.Equal(t, http.StatusOK, response.StatusCode, string(body))
				require.EqualValues(t, 1, secondCalls.Load())
				require.Equal(t, 3000, <-reservations)
				token, err := model.GetTokenById(fixture.token.Id)
				require.NoError(t, err)
				require.Equal(t, 7000, token.RemainQuota)
			}
			require.Eventually(t, func() bool {
				return service.ChannelInFlight(fixture.channel.Id) == 0 && service.ChannelInFlight(ch.Id) == 0
			}, 3*time.Second, 10*time.Millisecond)
			fixture.closeAndWait(t)
		})
	}
}
