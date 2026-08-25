package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestClassifyRouteErrorSeparatesKeyModelAndStreamFailures(t *testing.T) {
	assert.Equal(t, RouteErrorKey, ClassifyRouteError(401, "", "", false).Class)
	assert.True(t, ClassifyRouteError(401, "", "", false).MarkKey)
	assert.Equal(t, RouteErrorModel, ClassifyRouteError(404, "model_not_found", "", false).Class)
	assert.True(t, ClassifyRouteError(404, "model_not_found", "", false).MarkCapability)
	assert.Equal(t, RouteErrorStreamStarted, ClassifyRouteError(502, "", "", true).Class)
	assert.False(t, ClassifyRouteError(502, "", "", true).Failoverable)
}

func TestRouteHealthStateMachineUsesEpochAndCooldown(t *testing.T) {
	now := time.Unix(1000, 0)
	policy := RouteHealthPolicy{FailureThreshold: 2, Cooldown: 10 * time.Second}
	health := model.ChannelHealth{ChannelID: 1, Model: "gpt-5", KeyScope: "channel"}
	health = ObserveRouteHealthFailure(health, policy, now)
	assert.Equal(t, model.RouteHealthStateClosed, health.State)
	health = ObserveRouteHealthFailure(health, policy, now)
	assert.Equal(t, model.RouteHealthStateOpen, health.State)
	assert.Equal(t, int64(1010), health.CooldownUntil)
	assert.False(t, CanUseRouteHealth(health, now))
	health = EnterRouteHealthHalfOpen(health, time.Unix(1010, 0))
	assert.Equal(t, model.RouteHealthStateHalfOpen, health.State)
	health = ObserveRouteHealthSuccess(health, time.Unix(1011, 0))
	assert.Equal(t, model.RouteHealthStateClosed, health.State)
	assert.Equal(t, 0, health.FailureCount)
}

func TestDefaultRouteRetryBudgetSeparatesSameResourceAndFailover(t *testing.T) {
	budget := DefaultRouteRetryBudget()
	transient := ClassifyRouteError(503, "", "", false)
	assert.False(t, budget.Allows(transient, true, true, 0))
	assert.True(t, budget.Allows(transient, false, true, 0))
	assert.True(t, budget.Allows(transient, false, false, 1))
	assert.False(t, budget.Allows(transient, false, false, 3))
	assert.Equal(t, 125*time.Millisecond, RouteBackoff(0, 0, time.Second, 0.5))
	assert.LessOrEqual(t, RouteBackoff(4, 10*time.Second, 100*time.Millisecond, 1), 100*time.Millisecond)
}
