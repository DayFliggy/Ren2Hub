package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteAttemptHealthProbeOwnershipRecoveryAndStaleResults(t *testing.T) {
	db := setupRouteProfileTest(t)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: 91, Model: "gpt-test", State: model.RouteHealthStateOpen,
		CooldownUntil: 999, FailureCount: 3, HealthEpoch: 2,
	}).Error)
	for range 2 {
		usable, epoch, err := RouteHealthUsable(ctx, 91, "gpt-test", now)
		require.NoError(t, err)
		assert.True(t, usable)
		assert.Equal(t, int64(2), epoch, "planning must not claim a probe")
	}
	first, err := acquireRouteAttemptHealthAt(ctx, 91, "gpt-test", "", "first", time.Second, now)
	require.NoError(t, err)
	_, err = acquireRouteAttemptHealthAt(ctx, 91, "gpt-test", "", "competing", time.Second, now)
	require.ErrorIs(t, err, ErrRouteHealthUnavailable)
	require.NoError(t, renewRouteAttemptHealthAt(ctx, first, 2*time.Second, now.Add(500*time.Millisecond)))
	_, err = acquireRouteAttemptHealthAt(ctx, 91, "gpt-test", "", "too-early", time.Second, now.Add(time.Second))
	require.ErrorIs(t, err, ErrRouteHealthUnavailable)
	second, err := acquireRouteAttemptHealthAt(ctx, 91, "gpt-test", "", "second", time.Minute, now.Add(3*time.Second))
	require.NoError(t, err)
	assert.Greater(t, second.Scopes[0].Epoch, first.Scopes[0].Epoch)
	require.ErrorIs(t, renewRouteAttemptHealthAt(ctx, first, time.Minute, now.Add(3*time.Second)), ErrRouteHealthOwnership)
	require.NoError(t, observeRouteAttemptOutcomeAt(ctx, first, AttemptOutcome{Success: true}, now.Add(3*time.Second)))
	require.NoError(t, observeRouteAttemptOutcomeAt(ctx, first, AttemptOutcome{Origin: AttemptOriginProvider, StatusCode: 503}, now.Add(3*time.Second)))
	current, err := LoadRouteHealth(ctx, 91, "gpt-test")
	require.NoError(t, err)
	assert.Equal(t, "second", current.ProbeOwner)
	assert.Equal(t, second.Scopes[0].Epoch, current.HealthEpoch)
	require.NoError(t, ReleaseRouteAttemptHealth(ctx, second))
	third, err := acquireRouteAttemptHealthAt(ctx, 91, "gpt-test", "", "third", time.Minute, now.Add(3*time.Second))
	require.NoError(t, err)
	require.NoError(t, observeRouteAttemptOutcomeAt(ctx, third, AttemptOutcome{Success: true, LatencyMS: 25}, now.Add(4*time.Second)))
	current, err = LoadRouteHealth(ctx, 91, "gpt-test")
	require.NoError(t, err)
	assert.Equal(t, model.RouteHealthStateClosed, current.State)
	assert.Empty(t, current.ProbeOwner)
	assert.Zero(t, current.ProbeExpiresAt)
}

func TestRouteAttemptHealthLateSuccessAndShortFailureKeepNewerCooldown(t *testing.T) {
	setupRouteProfileTest(t)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	older, err := acquireRouteAttemptHealthAt(ctx, 92, "gpt-test", "", "older", time.Minute, now)
	require.NoError(t, err)
	newer, err := acquireRouteAttemptHealthAt(ctx, 92, "gpt-test", "", "newer", time.Minute, now)
	require.NoError(t, err)
	require.NoError(t, observeRouteAttemptOutcomeAt(ctx, newer, AttemptOutcome{
		Origin: AttemptOriginProvider, StatusCode: 429, RetryAfter: 2 * time.Minute,
	}, now))
	require.NoError(t, observeRouteAttemptOutcomeAt(ctx, older, AttemptOutcome{Success: true}, now.Add(time.Second)))
	require.NoError(t, observeRouteAttemptOutcomeAt(ctx, older, AttemptOutcome{
		Origin: AttemptOriginProvider, StatusCode: 503,
	}, now.Add(2*time.Second)))
	health, err := LoadRouteHealth(ctx, 92, "gpt-test")
	require.NoError(t, err)
	assert.Equal(t, model.RouteHealthStateOpen, health.State)
	assert.Equal(t, now.Add(2*time.Minute).Unix(), health.CooldownUntil)
	assert.Equal(t, 2, health.FailureCount)
}

func TestRouteAttemptHealthRollsBackPartialProbeAdmission(t *testing.T) {
	db := setupRouteProfileTest(t)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	require.NoError(t, db.Create(&[]model.ChannelHealth{
		{ChannelID: 93, Model: "gpt-test", State: model.RouteHealthStateOpen, CooldownUntil: 999, HealthEpoch: 1},
		{ChannelID: 93, Model: "gpt-test", KeyScope: RouteKeyScope("test-key"), State: model.RouteHealthStateOpen, CooldownUntil: 1100, HealthEpoch: 1},
	}).Error)
	_, err := acquireRouteAttemptHealthAt(ctx, 93, "gpt-test", "test-key", "rejected", time.Minute, now)
	require.ErrorIs(t, err, ErrRouteHealthUnavailable)
	health, err := LoadRouteHealth(ctx, 93, "gpt-test")
	require.NoError(t, err)
	assert.Empty(t, health.ProbeOwner)
	assert.True(t, RouteHealthReadOnlyUsable(health, now))
}

func TestRouteAttemptOutcomeSeparatesAttributionFromReplay(t *testing.T) {
	for _, origin := range []AttemptOrigin{AttemptOriginLocalConvert, AttemptOriginLocalParse, AttemptOriginAdmission, AttemptOriginClientCancel, ""} {
		t.Run(string(origin), func(t *testing.T) {
			classification := ClassifyAttemptOutcome(AttemptOutcome{Origin: origin, StatusCode: 503})
			assert.False(t, classification.MarkKey || classification.MarkCapability || classification.MarkChannelModel)
			assert.False(t, classification.Retryable || classification.Failoverable)
		})
	}
	setupRouteProfileTest(t)
	ctx := context.Background()
	attempt, err := AcquireRouteAttemptHealth(ctx, 94, "gpt-test", "", "committed", time.Minute)
	require.NoError(t, err)
	outcome := AttemptOutcome{Origin: AttemptOriginProvider, StatusCode: 503, ResponseCommitted: true}
	classification := ClassifyAttemptOutcome(outcome)
	assert.True(t, classification.MarkChannelModel)
	assert.False(t, classification.Retryable || classification.Failoverable)
	require.NoError(t, ObserveRouteAttemptOutcome(ctx, attempt, outcome))
	health, err := LoadRouteHealth(ctx, 94, "gpt-test")
	require.NoError(t, err)
	assert.Equal(t, 1, health.FailureCount)
	assert.True(t, ClassifyAttemptOutcome(AttemptOutcome{Origin: AttemptOriginProvider, StatusCode: 503, Message: "provider route lease unavailable"}).MarkChannelModel)
}
