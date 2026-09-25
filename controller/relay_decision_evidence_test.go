package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type routeEvidenceCapture struct {
	requestID string
	events    chan service.RouteDecision
}

func (capture *routeEvidenceCapture) Write(data []byte) (int, error) {
	line := string(data)
	if index := strings.IndexByte(line, '{'); index >= 0 {
		var event service.RouteDecision
		if common.Unmarshal([]byte(strings.TrimSpace(line[index:])), &event) == nil && event.RequestID == capture.requestID {
			capture.events <- event
		}
	}
	return len(data), nil
}

func TestFinalRouteEvidenceRetainsLastSendAfterRejectedCandidates(t *testing.T) {
	for _, test := range []struct {
		name     string
		attempts []int
		filtered bool
		want     int
	}{
		{name: "all channels enter cooldown", attempts: []int{11, 12}, filtered: true, want: 12},
		{name: "budget stops selected unsent replacement", attempts: []int{11}, want: 11},
		{name: "no actual send", filtered: true, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			capture := &routeEvidenceCapture{requestID: t.Name(), events: make(chan service.RouteDecision, 1)}
			common.LogWriterMu.Lock()
			original := gin.DefaultWriter
			gin.DefaultWriter = capture
			common.LogWriterMu.Unlock()
			t.Cleanup(func() {
				common.LogWriterMu.Lock()
				gin.DefaultWriter = original
				common.LogWriterMu.Unlock()
			})
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			decision := service.NewRouteDecision(t.Name(), service.RouteSourceAutoLab, "test-model", 1)
			decision.Candidates = []service.RouteDecisionCandidate{{ChannelID: 11}, {ChannelID: 12}}
			c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab, Decision: decision, Attempts: decision.Candidates})
			for _, id := range test.attempts {
				beginLiveRouteUpstreamAttempt(c, id)
			}
			markLiveRouteAttempt(c, 12, service.AdmissionStateAcquired)
			if test.filtered {
				markLiveRouteCandidateFiltered(c, 12, "channel_cooldown")
			}
			upstreamErr := types.NewErrorWithStatusCode(errors.New("upstream unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusServiceUnavailable)
			finalizeLiveRouteDecision(c, &relaycommon.RelayInfo{}, upstreamErr)
			select {
			case event := <-capture.events:
				assert.Equal(t, test.want, event.SelectedChannelID)
				assert.Equal(t, len(test.attempts), event.UpstreamAttempts)
				assert.Equal(t, max(0, len(test.attempts)-1), event.RetryAttempt)
				assert.NotEmpty(t, event.FinalError)
			case <-time.After(time.Second):
				require.FailNow(t, "final route decision was not emitted")
			}
		})
	}
}

func TestRouteAttemptEvidenceExcludesUnsentAndProviderText(t *testing.T) {
	capture := &routeEvidenceCapture{requestID: t.Name(), events: make(chan service.RouteDecision, 1)}
	common.LogWriterMu.Lock()
	original := gin.DefaultWriter
	gin.DefaultWriter = capture
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = original
		common.LogWriterMu.Unlock()
	})
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	selection := service.LiveRouteSelection{Source: service.RouteSourceAutoLab, Decision: service.NewRouteDecision(t.Name(), service.RouteSourceAutoLab, "test-model", 0)}
	c.Set("route_live_selection", selection)
	info := &relaycommon.RelayInfo{UpstreamSent: true}
	beginLiveRouteUpstreamAttempt(c, 11)
	observeLiveRouteAttempt(c, info, service.AttemptOutcome{
		Origin: service.AttemptOriginProvider, StatusCode: http.StatusServiceUnavailable,
		ProviderCode: "private-provider-code", Message: "private-provider-message", LatencyMS: 25, TTFTMS: 4,
		Acceptance: service.AttemptAcceptanceUnknown, RetryPermission: service.AttemptRetryPolicy,
	})
	info.UpstreamSent = false
	observeLiveRouteAttempt(c, info, service.AttemptOutcome{
		Origin: service.AttemptOriginAdmission, StatusCode: http.StatusTooManyRequests,
		Acceptance: service.AttemptNotSent, RetryPermission: service.AttemptRetryForbidden,
	})
	// Channel reselection can replace the selection, but not the logical history.
	c.Set("route_live_selection", selection)
	info.UpstreamSent = true
	beginLiveRouteUpstreamAttempt(c, 12)
	observeLiveRouteAttempt(c, info, service.AttemptOutcome{
		Origin: service.AttemptOriginProvider, Success: true, StatusCode: http.StatusCreated,
		LatencyMS: 12, Acceptance: service.AttemptAccepted, RetryPermission: service.AttemptRetryPolicy,
		ResponseCommitted: true,
	})
	finalizeLiveRouteDecision(c, info, nil)
	select {
	case decision := <-capture.events:
		require.Len(t, decision.Attempts, 2)
		assert.Equal(t, service.RouteDecisionAttempt{
			Sequence: 1, ChannelID: 11, ErrorClass: service.RouteErrorTransient,
			ErrorHTTPStatus: http.StatusServiceUnavailable, Origin: service.AttemptOriginProvider,
			LatencyMS: 25, Acceptance: service.AttemptAcceptanceUnknown, RetryPermission: service.AttemptRetryPolicy,
		}, decision.Attempts[0])
		assert.Equal(t, service.RouteDecisionAttempt{
			Sequence: 2, ChannelID: 12, Success: true, Origin: service.AttemptOriginProvider,
			LatencyMS: 12, Acceptance: service.AttemptAccepted, RetryPermission: service.AttemptRetryPolicy,
			ResponseCommitted: true,
		}, decision.Attempts[1])
		encoded, err := common.Marshal(decision)
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "private-provider")
		assert.NotContains(t, string(encoded), "ttft")
		assert.Equal(t, 2, decision.UpstreamAttempts)
		assert.Equal(t, 12, decision.SelectedChannelID)
	case <-time.After(time.Second):
		require.FailNow(t, "final route decision was not emitted")
	}
}
