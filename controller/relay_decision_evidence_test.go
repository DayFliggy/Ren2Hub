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
