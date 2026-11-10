package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func withTaskRetries(t *testing.T, retries int) {
	t.Helper()
	require.NoError(t, i18n.Init())
	previous := common.RetryTimes
	common.RetryTimes = retries
	t.Cleanup(func() { common.RetryTimes = previous })
}

// With no previous attempt the selection error is still the answer.
func TestTaskFirstAttemptWithoutCandidateReturnsSelectionError(t *testing.T) {
	withTaskRetries(t, 1)
	events := []string{}
	database := setupTaskSubmissionDatabase(t, true, &events)
	require.NoError(t, database.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
	info := taskSubmissionRelayInfo(&taskSubmissionTestBilling{events: &events})
	info.LockedChannel = nil
	called := false

	_, taskErr := executeTaskSubmissionWith(taskSubmissionTestContext(), info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		called = true
		return nil, nil
	})

	require.False(t, called)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusServiceUnavailable, taskErr.StatusCode)
	require.Equal(t, "get_channel_failed", taskErr.Code)
}

func taskObservationSamples(channelID int, modelName string) int {
	samples := 0
	for _, observation := range service.RoutingSnapshot().Observations {
		if observation.ChannelID == channelID && observation.Model == modelName {
			samples += observation.Samples
		}
	}
	return samples
}

// B13: each attempt is observed on its own. A retryable local rejection
// marks only its own attempt as a business rejection; before the fix the
// mark stayed set and the next attempt's upstream failure or success was not
// recorded for the channel's health (route samples and breaker share that
// outcome).
func TestTaskRetryObservesEachAttemptOnItsOwn(t *testing.T) {
	localBusy := &dto.TaskError{Code: "plugin_busy", Message: "plugin busy", StatusCode: http.StatusTooManyRequests, LocalError: true}
	for _, tc := range []struct {
		name   string
		second *dto.TaskError
	}{
		{"local rejection then upstream failure", &dto.TaskError{Code: "vendor_502", Message: "vendor unavailable", StatusCode: http.StatusBadGateway}},
		{"local rejection then success", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTaskRetries(t, 1)
			previousLogConsumeEnabled := common.LogConsumeEnabled
			common.LogConsumeEnabled = false
			t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })
			events := []string{}
			setupTaskSubmissionDatabase(t, true, &events)
			info := taskSubmissionRelayInfo(&taskSubmissionTestBilling{events: &events})
			before := taskObservationSamples(1, info.OriginModelName)
			c := taskSubmissionTestContext()
			common.SetContextKey(c, constant.ContextKeyChannelId, 1)
			attempts := 0

			_, taskErr := executeTaskSubmissionWith(c, info, func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				attempts++
				service.MarkRouteAttemptSent(c, info)
				if attempts == 1 {
					return nil, localBusy
				}
				if tc.second != nil {
					return nil, tc.second
				}
				return &relay.TaskSubmitResult{UpstreamTaskID: "upstream_private", Platform: "plugin"}, nil
			})

			require.Equal(t, 2, attempts)
			if tc.second != nil {
				require.Same(t, tc.second, taskErr)
			} else {
				require.Nil(t, taskErr)
			}
			require.Equal(t, 1, taskObservationSamples(1, info.OriginModelName)-before, "only the second attempt is a channel sample")
		})
	}
}

// B13: a stream outcome recorded by one attempt does not classify the next.
// The first attempt's stream failed upstream; the retry succeeds without a
// stream. The channel's observation is one failure then one success (EWMA
// 0.2*0 + 0.8*1); a leftover StreamStatus would count the retry as failed too.
func TestTaskRetryDoesNotReuseThePreviousStreamOutcome(t *testing.T) {
	withTaskRetries(t, 1)
	previousLogConsumeEnabled := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })
	events := []string{}
	setupTaskSubmissionDatabase(t, true, &events)
	info := taskSubmissionRelayInfo(&taskSubmissionTestBilling{events: &events})
	info.OriginModelName = "b13-stream-model" // an observation key no other test writes
	c := taskSubmissionTestContext()
	// ResolveOriginTask sets up the locked channel before the first attempt.
	common.SetContextKey(c, constant.ContextKeyChannelId, 1)
	attempts := 0

	_, taskErr := executeTaskSubmissionWith(c, info, func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		attempts++
		service.MarkRouteAttemptSent(c, info)
		if attempts == 1 {
			info.StreamStatus = relaycommon.NewStreamStatus()
			info.StreamStatus.MarkFailed("server_error", "server_error", http.StatusBadGateway)
			return nil, &dto.TaskError{Code: "vendor_502", Message: "vendor stream failed", StatusCode: http.StatusBadGateway}
		}
		return &relay.TaskSubmitResult{UpstreamTaskID: "upstream_private", Platform: "plugin"}, nil
	})

	require.Nil(t, taskErr)
	require.Equal(t, 2, attempts)
	var observed []service.RouteObservationSnapshot
	for _, observation := range service.RoutingSnapshot().Observations {
		if observation.ChannelID == 1 && observation.Model == info.OriginModelName {
			observed = append(observed, observation)
		}
	}
	require.Len(t, observed, 1)
	require.Equal(t, 2, observed[0].Samples)
	require.InDelta(t, 0.8, observed[0].ErrorRate, 1e-9, "the retry is a success sample")
}

// A4 is a local routing regression; keep the original vendor payload on exhaustion.
func TestTaskRetryWithoutCandidateKeepsPreviousTaskError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sent, local bool
	}{
		{"vendor", true, false}, {"wrapped_response", true, true},
		{"request_build_failure", false, false}, {"local_preflight", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTaskRetries(t, 1)
			events := []string{}
			setupTaskSubmissionDatabase(t, true, &events)
			billing := &taskSubmissionTestBilling{events: &events}
			info := taskSubmissionRelayInfo(billing)
			info.LockedChannel = nil
			attemptErr := &dto.TaskError{Code: "fail_to_fetch_task", Message: "attempt unavailable", StatusCode: http.StatusBadGateway, LocalError: tc.local, Data: map[string]any{"trace": "v-1"}}
			c := taskSubmissionTestContext()
			channel, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			c.Set("route_selected_channel", channel)
			attempts := 0
			_, taskErr := executeTaskSubmissionWith(c, info, func(c *gin.Context, info *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				attempts++
				if tc.sent {
					service.MarkRouteAttemptSent(c, info)
				}
				return nil, attemptErr
			})
			require.Equal(t, 1, attempts, "the only channel was already used")
			if tc.sent {
				require.Same(t, attemptErr, taskErr)
			} else {
				require.Equal(t, "get_channel_failed", taskErr.Code, "pre-dispatch errors do not acquire upstream error priority")
			}
			require.Equal(t, 1, billing.refunds)
			require.Zero(t, service.ChannelInFlight(1))
		})
	}
}
