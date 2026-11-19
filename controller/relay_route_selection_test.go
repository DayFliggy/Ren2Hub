package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// postResponsesThroughGateway sends one HTTP Responses request through the
// fixture's TokenAuth -> Distribute -> Relay chain and returns status and body.
// It does not wait for Relay: a request rejected by Distribute never reaches it.
func postResponsesThroughGateway(t *testing.T, fixture *responsesWSBillingTest) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, fixture.gatewayURL+"/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	return response.StatusCode, string(body)
}

func waitRelayFinished(t *testing.T, fixture *responsesWSBillingTest) {
	t.Helper()
	select {
	case <-fixture.httpDone:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP request did not finish")
	}
}

// B2: a retry round without another candidate must not replace the real
// upstream failure with "no available channel".
func TestRelayRetryWithoutCandidateKeepsUpstreamError(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	oldRetries := common.RetryTimes
	common.RetryTimes = 1
	t.Cleanup(func() { common.RetryTimes = oldRetries })
	var attempts atomic.Int64
	fixture.httpUpstream = func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":{"type":"server_error","code":"server_error","message":"upstream exploded"}}`)
	}
	status, body := postResponsesThroughGateway(t, fixture)
	waitRelayFinished(t, fixture)
	require.Equal(t, int64(1), attempts.Load(), "the only channel was already used")
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, body, "upstream exploded")
	require.NotContains(t, body, "No available channel")
}

// captureRouteErrors takes a synchronized snapshot while the mock gateway is running.
func captureRouteErrors(t *testing.T) func() string {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "route-errors-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, output.Close()) })
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		defer common.LogWriterMu.Unlock()
		gin.DefaultErrorWriter = oldWriter
	})
	return func() string {
		common.LogWriterMu.Lock()
		defer common.LogWriterMu.Unlock()
		data, err := os.ReadFile(output.Name())
		require.NoError(t, err)
		return string(data)
	}
}

func TestRelayFullScanIsTraceableWithoutErrorLog(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	ch := fixture.channel
	require.NoError(t, model.DB.Model(ch).Update("max_concurrency", 1).Error)
	require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), ch.Id))
	permit, err := service.AcquireChannelConcurrency(context.Background(), ch)
	require.NoError(t, err)
	t.Cleanup(permit.Release)
	previous := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = false
	t.Cleanup(func() { constant.ErrorLogEnabled = previous })

	engine := gin.New()
	engine.POST("/v1/responses", middleware.RequestId(), middleware.TokenAuth(), middleware.Distribute(), func(c *gin.Context) {
		t.Fatal("full channel reached the relay")
	})
	readLog := captureRouteErrors(t)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, http.StatusTooManyRequests, response.Code)
	require.Contains(t, response.Body.String(), "channel_concurrency_exceeded")
	require.NotContains(t, response.Body.String(), "full_skips")
	requestID := response.Header().Get(common.RequestIdKey)
	require.NotEmpty(t, requestID)
	logged := readLog()
	require.Equal(t, 1, strings.Count(logged, "route_scan "))
	require.Contains(t, logged, requestID+" | route_scan round=1 candidates=1 full_skips=1 result=full")
	var count int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", requestID).Count(&count).Error)
	require.Zero(t, count)
}

func TestRelayScanSummaryIsAdminOnly(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})
	fixture.httpUpstream = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"ok","status":"completed","usage":{"input_tokens":10,"output_tokens":1,"total_tokens":11}}`)
	}
	status, _ := postResponsesThroughGateway(t, fixture)
	waitRelayFinished(t, fixture)
	require.Equal(t, http.StatusOK, status)
	filter := model.LogFilter{Type: model.LogTypeConsume, RequestId: "responses-http-billing"}
	admin, total, err := model.GetAllLogs(filter, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	model.FormatAdminLogs(admin)
	require.Contains(t, admin[0].Other, `"full_skips":0`)
	require.Contains(t, admin[0].Other, `"reason":"selected"`)
	require.Contains(t, admin[0].Other, fmt.Sprintf(`"ranked":[{"channel_id":%d,`, fixture.channel.Id))
	user, total, err := model.GetUserLogs(fixture.user.Id, filter, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.NotContains(t, user[0].Other, "request_policy")
	require.NotContains(t, user[0].Other, "full_skips")
	require.NotContains(t, user[0].Other, "ranked")
}

func TestRelayPrechargeRejectionKeepsSelectedScan(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(1000))`, func(*websocket.Conn, *http.Request) {})
	// Open the breaker in virtual time far enough in the past for this real
	// request to own the half-open probe when the billing check rejects it.
	synctest.Test(t, func(t *testing.T) {
		for range 5 {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			common.SetContextKey(c, constant.ContextKeyChannelId, fixture.channel.Id)
			service.BeginRouteObservation(c)
			info := &relaycommon.RelayInfo{OriginModelName: "ws-billing"}
			service.MarkRouteAttemptSent(c, info)
			service.FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("fixture failure"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway))
		}
	})
	t.Cleanup(func() {
		require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), fixture.channel.Id))
	})
	checkHalfOpen := func() {
		breakers := service.RoutingSnapshot().Breakers
		index := slices.IndexFunc(breakers, func(b service.RouteBreakerSnapshot) bool {
			return b.ChannelID == fixture.channel.Id && b.Model == "ws-billing"
		})
		require.NotEqual(t, -1, index)
		require.Equal(t, "half_open", breakers[index].State)
		require.False(t, breakers[index].ProbeInFlight)
	}
	checkHalfOpen()

	var calls atomic.Int32
	fixture.httpUpstream = func(http.ResponseWriter, *http.Request) { calls.Add(1) }
	readLog := captureRouteErrors(t)
	status, body := postResponsesThroughGateway(t, fixture)
	waitRelayFinished(t, fixture)
	require.Equal(t, http.StatusForbidden, status, body)
	checkHalfOpen()
	require.Zero(t, service.ChannelInFlight(fixture.channel.Id))
	require.Zero(t, calls.Load())
	require.NotContains(t, body, "full_skips")
	logged := readLog()
	require.Equal(t, 1, strings.Count(logged, "route_scan "))
	require.Contains(t, logged, "responses-http-billing | route_scan round=1 candidates=1 full_skips=0 result=selected")
	require.Contains(t, logged, fmt.Sprintf("channel_id=%d request_status=403", fixture.channel.Id))
	var rows int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("request_id = ?", "responses-http-billing").Count(&rows).Error)
	require.Zero(t, rows)
}

func TestRelayRoutingStorageFailuresAreTraceable(t *testing.T) {
	for _, tc := range []struct {
		name, table, marker string
		limit               bool
		status              int
	}{
		{"token scope", "token_route_profiles", "route_scope stage=token_route_scope result=error", false, 500},
		{"candidate lookup", "channels", "route_scan round=1 candidates=0 full_skips=0 result=error", false, 500},
		{"slot limit", "channels", "route_scan round=1 candidates=1 full_skips=0 result=error", true, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newResponsesWSBillingTest(t, `tier("request", fixed(0.002))`, func(*websocket.Conn, *http.Request) {})

			queryCallbacks := model.DB.Callback().Query()
			require.NoError(t, queryCallbacks.Before("gorm:query").Register("test:route-store", func(tx *gorm.DB) {
				if tx.Statement.Table == tc.table && (!tc.limit || slices.Contains(tx.Statement.Selects, "max_concurrency")) {
					tx.AddError(errors.New("private routing storage error"))
				}
			}))
			t.Cleanup(func() { require.NoError(t, queryCallbacks.Remove("test:route-store")) })
			if tc.limit {
				require.Error(t, service.RefreshChannelConcurrencyLimit(context.Background(), fixture.channel.Id))
			}
			engine := gin.New()
			engine.POST("/v1/responses", middleware.RequestId(), middleware.TokenAuth(), middleware.Distribute(), func(*gin.Context) { t.Fatal("storage failure reached relay") })
			request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"ws-billing","input":"hello"}`))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer sk-"+fixture.token.Key)
			response := httptest.NewRecorder()
			readLog := captureRouteErrors(t)
			engine.ServeHTTP(response, request)
			require.Equal(t, tc.status, response.Code)
			id := response.Header().Get(common.RequestIdKey)
			require.NotEmpty(t, id)
			logged := readLog()
			require.Equal(t, 1, strings.Count(logged, id+" | "+tc.marker))
			if tc.name == "token scope" {
				require.NotContains(t, logged, "route_scan ")
			}
			require.NotContains(t, response.Body.String(), "private routing storage error")
			require.NotContains(t, response.Body.String(), "route_scan")
		})
	}
}

func TestResponsesWSPrechargeRejectionKeepsSelectedScan(t *testing.T) {
	fixture := newResponsesWSBillingTest(t, `tier("request", fixed(1000))`, func(*websocket.Conn, *http.Request) {})
	readLog := captureRouteErrors(t)
	require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)))
	response := readResponsesWSTestEvent(t, fixture.client)
	require.Equal(t, "error", response["type"])
	require.Zero(t, fixture.connections.Load())
	require.NotContains(t, fmt.Sprint(response), "full_skips")
	logged := readLog()
	require.Equal(t, 1, strings.Count(logged, "route_scan "))
	require.Contains(t, logged, "responses-ws-billing-ws-0 | route_scan round=1 candidates=1 full_skips=0 result=selected")
	require.Contains(t, logged, fmt.Sprintf("channel_id=%d request_status=403", fixture.channel.Id))
}
