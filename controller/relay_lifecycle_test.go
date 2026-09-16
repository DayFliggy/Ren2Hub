package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteSendFencePreservesLastUpstreamFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	previous := types.NewErrorWithStatusCode(errors.New("upstream unavailable"), types.ErrorCodeBadResponseStatusCode, http.StatusBadGateway)
	c.Set("gateway_last_upstream_error", previous)
	c.Set(liveRouteAttemptCountKey, service.DefaultTotalAttempts)
	info := &relaycommon.RelayInfo{}
	err := prepareLiveRouteSend(c, info, &routeAttemptHealthState{}, "blocked", nil)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, previous.StatusCode, apiErr.StatusCode)
	assert.Equal(t, previous.GetErrorCode(), apiErr.GetErrorCode())
	assert.Equal(t, previous.Error(), apiErr.Error())
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.False(t, types.IsSkipRetryError(previous))
	assert.False(t, info.UpstreamSent)
	assert.Equal(t, service.DefaultTotalAttempts, c.GetInt(liveRouteAttemptCountKey))
	outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
	assert.Equal(t, service.AttemptOriginAdmission, outcome.Origin)
	assert.Equal(t, service.AttemptNotSent, outcome.Acceptance)
	assert.False(t, service.ClassifyAttemptOutcome(outcome).MarkChannelModel)
}

func TestRouteRetryAfterIsScopedToFailedChannel(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab, Attempts: []service.RouteDecisionCandidate{{ChannelID: 11}, {ChannelID: 12}}})
	assert.Equal(t, time.Hour, liveRouteScopedRetryAfter(c, 0, 11, time.Hour))
	assert.Zero(t, liveRouteScopedRetryAfter(c, 1, 11, time.Hour))
}

func TestRouteBackoffReleasesPermitAndUsesLogicalContext(t *testing.T) {
	manager := service.NewAdmissionManager(10)
	permit, err := manager.TryAcquire(context.Background(), "channel:31", service.AdmissionPolicy{Capacity: 1})
	require.NoError(t, err)
	permit.ChannelID = 31
	parent, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	attempt, cancel := context.WithCancel(parent)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(attempt)
	c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab})
	c.Set("route_channel_permit", permit)
	c.Set("route_attempt_cancel", context.CancelFunc(cancel))
	c.Set("route_attempt_parent_context", parent)
	require.True(t, waitForLiveRouteBackoff(c, 0, 0))
	assert.ErrorIs(t, attempt.Err(), context.Canceled)
	assert.NoError(t, parent.Err())
	assert.Zero(t, manager.Snapshot("channel:31").InFlight)
	assert.Same(t, parent, c.Request.Context())
	// A provider's lower bound cannot be clipped to make one more send.
	assert.False(t, waitForLiveRouteBackoff(c, 0, time.Hour))
}

func TestRouteRetryBudgetStopKeepsProviderErrorOutOfHealth(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	previous := types.NewErrorWithStatusCode(errors.New("rate limited by A"), types.ErrorCodeBadResponseStatusCode, http.StatusTooManyRequests)
	previous.RetryAfter = time.Minute
	c.Set("gateway_last_upstream_error", previous)
	c.Set("gateway_retry_budget_exhausted", true)
	err := stopLiveRouteRetry(c, service.ErrGatewayRetryBudget)
	assert.Equal(t, http.StatusTooManyRequests, err.StatusCode)
	assert.Equal(t, time.Minute, err.RetryAfter)
	assert.Equal(t, previous.Error(), err.Error())
	outcome := relayAttemptOutcome(c, &relaycommon.RelayInfo{}, err, time.Now(), false)
	classification := service.ClassifyAttemptOutcome(outcome)
	assert.False(t, classification.MarkChannelModel)
	assert.False(t, classification.Failoverable)
}

func TestRouteQueueSelectionKeepsPolicyBoundariesAndOneQueue(t *testing.T) {
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
	for _, id := range []int{7701, 7702} {
		require.NoError(t, db.Create(&model.Channel{Id: id, Status: common.ChannelStatusEnabled, CapacityTotal: 1, ChannelRatio: 1}).Error)
		permit, _, err := service.AcquireGatewayChannelPermit(context.Background(), "holder", id, false, service.NewGatewayWaitBudget(0))
		require.NoError(t, err)
		defer permit.Release()
	}
	queuedCtx, cancelQueued := context.WithCancel(context.Background())
	defer cancelQueued()
	queuedDone := make(chan error, 1)
	go func() {
		permit, _, err := service.AcquireGatewayChannelPermit(queuedCtx, "old-waiter", 7701, true, service.NewGatewayWaitBudget(time.Second*5))
		if permit != nil {
			permit.Release()
		}
		queuedDone <- err
	}()
	require.Eventually(t, func() bool { return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(7701)).Queued == 1 }, time.Second, time.Millisecond)
	defer func() { cancelQueued(); <-queuedDone }()
	policy, err := service.LoadGatewayAdmissionPolicy(context.Background(), 1)
	require.NoError(t, err)
	tests := []struct {
		name                   string
		source                 service.RouteSource
		dynamic, balance       bool
		priority               int64
		bucket, position, want int
	}{
		{name: "auto same layer", source: service.RouteSourceAutoLab, dynamic: true, want: 1},
		{name: "auto priority boundary", source: service.RouteSourceAutoLab, dynamic: true, priority: 1, want: 0},
		{name: "auto bucket boundary", source: service.RouteSourceAutoLab, dynamic: true, bucket: 1, want: 0},
		{name: "auto static", source: service.RouteSourceAutoLab, want: 0},
		{name: "manual balanced", source: service.RouteSourceManual, balance: true, want: 1},
		{name: "manual position", source: service.RouteSourceManual, balance: true, position: 1, want: 0},
		{name: "manual static", source: service.RouteSourceManual, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx := service.WithGatewayAdmissionContext(context.Background(), policy, service.NewGatewayWaitBudget(time.Second))
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			c.Set("route_live_selection", service.LiveRouteSelection{Source: test.source, ManualLoadBalance: test.balance, Decision: service.RouteDecision{DynamicScoreApplied: test.dynamic}, Attempts: []service.RouteDecisionCandidate{
				{ChannelID: 7701, RequestModel: "test", FilterReason: "capacity_exhausted", Score: &service.RouteScoreBreakdown{}},
				{ChannelID: 7702, RequestModel: "test", FilterReason: "capacity_exhausted", Priority: test.priority, Position: test.position, Score: &service.RouteScoreBreakdown{LoadBucket: test.bucket}},
			}})
			index, selected := queueBusyLiveRouteCandidate(c)
			require.True(t, selected)
			assert.Equal(t, test.want, index)
			_, selected = queueBusyLiveRouteCandidate(c)
			assert.False(t, selected, "one logical request cannot select a second channel queue")
		})
	}
	require.NoError(t, db.Create(&model.ChannelHealth{ChannelID: 7702, Model: "test", State: model.RouteHealthStateOpen, HealthEpoch: 1, CooldownUntil: time.Now().Add(time.Minute).Unix()}).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := service.WithGatewayAdmissionContext(context.Background(), policy, service.NewGatewayWaitBudget(time.Second))
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceManual, ManualLoadBalance: true, Attempts: []service.RouteDecisionCandidate{{ChannelID: 7701, RequestModel: "test", FilterReason: "capacity_exhausted"}, {ChannelID: 7702, RequestModel: "test", FilterReason: "capacity_exhausted"}}})
	index, selected := queueBusyLiveRouteCandidate(c)
	require.True(t, selected)
	assert.Zero(t, index, "new cooldown must remove the lower-pressure queue from consideration")

	// A previously busy channel can become immediately available while the
	// other candidates are scanned. Immediate capacity precedes any queue.
	require.NoError(t, db.Create(&model.Channel{Id: 7703, Status: common.ChannelStatusEnabled, CapacityTotal: 1, ChannelRatio: 1}).Error)
	c, _ = gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab, Decision: service.RouteDecision{DynamicScoreApplied: true}, Attempts: []service.RouteDecisionCandidate{
		{ChannelID: 7701, RequestModel: "test", FilterReason: "capacity_exhausted", Score: &service.RouteScoreBreakdown{}},
		{ChannelID: 7703, RequestModel: "test", FilterReason: "capacity_exhausted", Priority: -1, Score: &service.RouteScoreBreakdown{}},
	}})
	index, selected = queueBusyLiveRouteCandidate(c)
	require.True(t, selected)
	assert.Equal(t, 1, index, "a free lower-priority candidate is attempted before queueing on a busy one")
}

func TestRouteQueueSelectionUsesCurrentPolicyAndReclaimedCapacity(t *testing.T) {
	for _, test := range []struct {
		name          string
		before, after int
		release       bool
		want          bool
	}{
		{name: "queue enabled", before: 0, after: 20, want: true},
		{name: "queue disabled", before: 20, after: 0, want: false},
		{name: "reclaimed idle capacity", before: 20, after: 20, release: true, want: true},
		{name: "idle capacity needs no queue", before: 0, after: 0, release: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _ := setupGatewayAPI(t)
			require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
			require.NoError(t, db.Create(&model.Channel{Id: 7731, Status: common.ChannelStatusEnabled, CapacityTotal: 1, ChannelRatio: 1}).Error)
			config := model.DefaultGatewayConfig()
			config.ChannelQueueSize = test.before
			require.NoError(t, model.SaveGatewayConfig(config))
			policy, err := service.LoadGatewayAdmissionPolicy(context.Background(), 1)
			require.NoError(t, err)
			holder, _, err := service.AcquireGatewayChannelPermit(context.Background(), "busy", 7731, false, nil)
			require.NoError(t, err)
			defer holder.Release()
			config, err = model.GetGatewayConfigContext(context.Background())
			require.NoError(t, err)
			config.ChannelQueueSize = test.after
			require.NoError(t, model.SaveGatewayConfig(config))
			if test.release {
				holder.Release()
				assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(7731)).Capacity)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx := service.WithGatewayAdmissionContext(context.Background(), policy, service.NewGatewayWaitBudget(time.Second))
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			c.Set("route_live_selection", service.LiveRouteSelection{Source: service.RouteSourceAutoLab, Attempts: []service.RouteDecisionCandidate{{ChannelID: 7731, RequestModel: "test", FilterReason: "capacity_exhausted"}}})
			_, selected := queueBusyLiveRouteCandidate(c)
			assert.Equal(t, test.want, selected)
			assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(7731)).Queued, "selection alone never creates a waiter")
		})
	}
}

func TestRouteAvailabilityUsesRaisedProviderLimit(t *testing.T) {
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.ChannelHealth{}))
	require.NoError(t, db.Create(&model.Channel{Id: 7732, Status: common.ChannelStatusEnabled, CapacityTotal: 1, ChannelRatio: 1}).Error)
	config := model.DefaultGatewayConfig()
	config.ChannelRates[7732] = model.GatewayProviderRateLimit{RPM: 1}
	require.NoError(t, model.SaveGatewayConfig(config))
	policy, err := service.LoadGatewayAdmissionPolicy(context.Background(), 1)
	require.NoError(t, err)
	reservation, err := service.ReserveGatewayProviderRate(context.Background(), common.RDB,
		[]service.GatewayProviderRateScope{{ID: service.GatewayChannelRateScopeID(7732), RPM: 1}}, 0)
	require.NoError(t, err)
	require.NoError(t, service.FinishGatewayProviderRate(context.Background(), common.RDB, reservation, 0, true, true, 0))
	config, err = model.GetGatewayConfigContext(context.Background())
	require.NoError(t, err)
	config.ChannelRates[7732] = model.GatewayProviderRateLimit{RPM: 2}
	require.NoError(t, model.SaveGatewayConfig(config))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx := service.WithGatewayAdmissionContext(context.Background(), policy, service.NewGatewayWaitBudget(time.Second))
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	assert.Empty(t, knownRouteCandidateUnavailable(c, service.RouteDecisionCandidate{ChannelID: 7732, RequestModel: "test"}))
}
