package controller

import (
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestDiagnosticAttemptsDistinguishPreparationHeadersAndValidOutput(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab})
	c.Set(liveRouteAttemptCountKey, 1)
	c.Set(liveRoutePreviousChannelKey, 7)
	c.Set("gateway_channel_queue_ms", int64(9))
	info := &relaycommon.RelayInfo{RequestId: "request", BillingAttemptID: "frozen-attempt", GatewayCredentialID: "credential", StartTime: time.Now().Add(-2 * time.Second), UpstreamSentAt: time.Now().Add(-time.Second)}
	outcome := service.AttemptOutcome{Origin: service.AttemptOriginProvider, LatencyMS: 5000, Acceptance: service.AttemptAcceptanceUnknown, RetryPermission: service.AttemptRetryPolicy}
	observeLiveRouteAttempt(c, info, outcome)
	_, exists := c.Get(liveRouteAttemptEvidenceKey)
	assert.False(t, exists, "unsent candidates must not become provider attempts")
	info.UpstreamSent = true
	info.SetFirstResponseTime()
	observeLiveRouteAttempt(c, info, outcome)
	value, _ := c.Get(liveRouteAttemptEvidenceKey)
	attempts, ok := value.([]service.RouteDecisionAttempt)
	require.True(t, ok)
	require.Len(t, attempts, 1)
	first := attempts[0]
	assert.Equal(t, "frozen-attempt", first.AttemptID)
	assert.Equal(t, "credential", first.CredentialID)
	assert.EqualValues(t, 9, first.QueueMS)
	assert.EqualValues(t, 5000, first.LatencyMS, "existing attempt latency retains preparation semantics")
	require.NotNil(t, first.UpstreamLatencyMS)
	assert.Less(t, *first.UpstreamLatencyMS, first.LatencyMS)
	assert.Nil(t, first.FirstOutputAt, "headers do not prove semantic output")
	assert.Nil(t, first.TTFTMS)
	c.Set(liveRouteAttemptCountKey, 2)
	info.BillingAttemptID = "next-attempt"
	info.MarkValidOutput()
	validAt, known := info.FirstValidOutputTime()
	require.True(t, known)
	observeLiveRouteAttempt(c, info, service.AttemptOutcome{Success: true, Origin: service.AttemptOriginProvider, Acceptance: service.AttemptAccepted, RetryPermission: service.AttemptRetryForbidden})
	value, _ = c.Get(liveRouteAttemptEvidenceKey)
	attempts = value.([]service.RouteDecisionAttempt)
	require.Len(t, attempts, 2)
	last := attempts[1]
	require.NotNil(t, last.FirstOutputAt)
	require.NotNil(t, last.TTFTMS)
	assert.Equal(t, validAt.UnixMilli(), *last.FirstOutputAt)
	assert.Equal(t, validAt.Sub(info.UpstreamSentAt).Milliseconds(), *last.TTFTMS)
	assert.Equal(t, "next-attempt", last.AttemptID)
}
