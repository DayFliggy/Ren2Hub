package controller

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

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
