package service

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// A free token count must not move the channel health that orders paid
// generation requests: failures, 429 cooldowns and successes are all ignored,
// while the channel slot is still released. The generation control proves the
// same sequence does reach the store.
func TestNativeCountAttemptsLeaveRouteHealthUntouched(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    int
		touches bool
	}{
		{"native count", relayconstant.RelayModeNativeCount, false},
		{"generation", relayconstant.RelayModeChatCompletions, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupRouteTest(t)
			resetRouteObservations(t)
			c := routeTestContext(t)
			BeginRouteObservation(c)
			common.SetContextKey(c, constant.ContextKeyChannelId, 11)
			info := &relaycommon.RelayInfo{OriginModelName: "model", RelayMode: tc.mode, StartTime: time.Now()}
			limited := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": []string{"60"}}}
			for _, status := range []int{http.StatusInternalServerError, http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusInternalServerError} {
				MarkRouteAttemptSent(c, info)
				if status == http.StatusTooManyRequests {
					ObserveRouteResponse(c, limited)
					ObserveRouteResponse(c, &http.Response{StatusCode: http.StatusTooManyRequests})
				}
				FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("upstream"), types.ErrorCodeBadResponseStatusCode, status))
			}
			health, _ := routeObservations.routeHealth(11, "model")
			if !tc.touches {
				require.Zero(t, health)
				require.Empty(t, routeObservations.breakers)
				require.Empty(t, routeObservations.cooldowns)
				require.Empty(t, routeObservations.snapshot())
				MarkRouteAttemptSent(c, info)
				FinishRouteAttempt(c, info, nil)
				require.Empty(t, routeObservations.snapshot(), "a count success must not dilute real failures either")
				return
			}
			require.Equal(t, routeWorstHealth, health)
			require.NotEmpty(t, routeObservations.cooldowns)
			require.Equal(t, 6, routeObservations.get(11, "model", relayconstant.RelayModeChatCompletions, false).Samples)
		})
	}
}
