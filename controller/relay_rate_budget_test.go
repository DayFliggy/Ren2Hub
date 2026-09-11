package controller

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/execution"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReserveAttemptRateBudgetUsesFinalOutboundCeiling(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	oldEnabled, oldClient := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = oldEnabled, oldClient
		_ = client.Close()
	})

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 71}}
	info.SetEstimatePromptTokens(100)
	info.SetEstimateOutputCeiling(200)
	info.UpdateEstimateOutputCeilingFromJSON([]byte(`{"max_completion_tokens":900}`))
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	common.SetContextKey(c, constant.ContextKeyObservedTotalTokens, 123)
	relaychannel.RequestExecution(c, execution.Generation)

	require.NoError(t, reserveAttemptRateBudget(c, info, model.Channel{Id: 71, ChannelTPM: 2000}))
	heldValue, exists := c.Get(liveRouteRateReservationKey)
	require.True(t, exists)
	held, valid := heldValue.(attemptRateReservation)
	require.True(t, valid)
	assert.Equal(t, 1000, held.reservation.ReservedTokens)
	assert.False(t, common.GetContextKeyBool(c, constant.ContextKeyLocalCountTokens))
	assert.Equal(t, -1, observedAttemptTokens(c))

	require.NoError(t, service.ReleaseChannelRateBudget(context.Background(), held.reservation, false))
}

func TestRateBudgetRejectionAdvancesUntilCandidatesExhausted(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source: service.RouteSourceAutoLab,
		Attempts: []service.RouteDecisionCandidate{
			{ChannelID: 101},
			{ChannelID: 102},
		},
		Decision: service.RouteDecision{Candidates: []service.RouteDecisionCandidate{
			{ChannelID: 101},
			{ChannelID: 102},
		}},
	})
	info := &relaycommon.RelayInfo{}
	state := relaychannel.RequestExecution(c, execution.Generation)

	nextAttempt, retry, apiErr := handleAttemptRateBudgetAdmissionError(c, info, 101, 0, service.ErrChannelRPM)
	require.True(t, retry)
	assert.Equal(t, 1, nextAttempt)
	assert.Nil(t, apiErr)
	assert.Zero(t, state.CallCount())

	selectionValue, exists := c.Get("route_live_selection")
	require.True(t, exists)
	selection := selectionValue.(service.LiveRouteSelection)
	assert.Equal(t, "rate_budget_exhausted", selection.Attempts[0].FilterReason)
	assert.Empty(t, selection.Attempts[1].FilterReason)

	_, retry, apiErr = handleAttemptRateBudgetAdmissionError(c, info, 102, nextAttempt, service.ErrChannelTPM)
	assert.False(t, retry)
	require.NotNil(t, apiErr)
	assert.Equal(t, 503, apiErr.StatusCode)
	assert.ErrorIs(t, apiErr, service.ErrRouteSelectionUnavailable)
	assert.Zero(t, state.CallCount())

	selectionValue, exists = c.Get("route_live_selection")
	require.True(t, exists)
	selection = selectionValue.(service.LiveRouteSelection)
	assert.Equal(t, "rate_budget_exhausted", selection.Attempts[0].FilterReason)
	assert.Equal(t, "rate_budget_exhausted", selection.Attempts[1].FilterReason)
}

func TestLiveRouteNeedsTokenAdmissionOnlyForTPMCandidates(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceAutoLab,
		Attempts: []service.RouteDecisionCandidate{{ChannelID: 301}, {ChannelID: 302}},
	})

	assert.False(t, liveRouteNeedsTokenAdmission(c))
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceAutoLab,
		Attempts: []service.RouteDecisionCandidate{{ChannelID: 301}, {ChannelID: 302, ChannelTPM: 1000}},
	})
	assert.True(t, liveRouteNeedsTokenAdmission(c))
}
