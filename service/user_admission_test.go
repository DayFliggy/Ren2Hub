package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserAdmissionFIFOAcrossClientsAndHeadCancellation(t *testing.T) {
	server := miniredis.RunT(t)
	first := redis.NewClient(&redis.Options{Addr: server.Addr()})
	second := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = first.Close(); _ = second.Close() })
	ctx := context.Background()
	policy := model.EffectiveUserLimits{Concurrency: 1, UserPolicyVersion: 1, ClassPolicyVersion: 1}
	status, active, err := userAdmissionStep(ctx, first, 1, "active", "active", policy)
	require.NoError(t, err)
	assert.Equal(t, 1, status)
	status, _, err = userAdmissionStep(ctx, second, 1, "first", "first-ticket", policy)
	require.NoError(t, err)
	assert.Zero(t, status)
	status, _, err = userAdmissionStep(ctx, first, 1, "second", "second-ticket", policy)
	require.NoError(t, err)
	assert.Zero(t, status)
	require.NoError(t, ReleaseRouteLease(ctx, first, active))
	status, _, err = userAdmissionStep(ctx, first, 1, "newcomer", "new-ticket", policy)
	require.NoError(t, err)
	assert.Zero(t, status, "new requests must not overtake existing waiters")
	require.NoError(t, cancelUserAdmissionTicket(ctx, second, 1, "first-ticket"))
	status, lease, err := userAdmissionStep(ctx, first, 1, "second", "second-ticket", policy)
	require.NoError(t, err)
	assert.Equal(t, 1, status)
	require.NoError(t, ReleaseRouteLease(ctx, first, lease))
}

func TestUserAdmissionRPMCountsOnceBeforeQueueAndDoesNotRefund(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	policy := model.EffectiveUserLimits{Concurrency: 1, RPM: 2}
	_, active, err := userAdmissionStep(ctx, client, 2, "active", "active", policy)
	require.NoError(t, err)
	for range 2 {
		status, _, err := userAdmissionStep(ctx, client, 2, "queued", "queued", policy)
		require.NoError(t, err)
		assert.Zero(t, status)
	}
	require.NoError(t, cancelUserAdmissionTicket(ctx, client, 2, "queued"))
	require.NoError(t, ReleaseRouteLease(ctx, client, active))
	status, _, err := userAdmissionStep(ctx, client, 2, "third", "third", policy)
	require.NoError(t, err)
	assert.Equal(t, -2, status, "cancelled queue entries still consume user RPM")
}

func TestUserAdmissionUnlimitedToLimitedAndDeadQueueHead(t *testing.T) {
	server := miniredis.RunT(t)
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	server.SetTime(now)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()
	policy := model.EffectiveUserLimits{Concurrency: 0}
	_, active, err := userAdmissionStep(ctx, client, 3, "active", "active", policy)
	require.NoError(t, err)
	policy.Concurrency = 1
	policy.UserPolicyVersion = 2
	status, _, err := userAdmissionStep(ctx, client, 3, "dead", "dead", policy)
	require.NoError(t, err)
	assert.Zero(t, status)
	require.NoError(t, ReleaseRouteLease(ctx, client, active))
	server.SetTime(now.Add(31 * time.Second))
	status, _, err = userAdmissionStep(ctx, client, 3, "new", "new", policy)
	require.NoError(t, err)
	assert.Equal(t, 1, status, "a process-death ticket expires without blocking future requests")
}
