package controller

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"

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
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	oldWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		defer common.LogWriterMu.Unlock()
		gin.DefaultErrorWriter = oldWriter
	})
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
	common.LogWriterMu.Lock()
	logged := output.String()
	common.LogWriterMu.Unlock()
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
	user, total, err := model.GetUserLogs(fixture.user.Id, filter, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.NotContains(t, user[0].Other, "request_policy")
	require.NotContains(t, user[0].Other, "full_skips")
}
