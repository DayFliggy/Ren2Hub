package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayPolicyPrecedencePreservesExplicitZero(t *testing.T) {
	config := model.DefaultGatewayConfig()
	groupLimit, userLimit, disabled := 6, 3, 0
	config.Groups["team"] = model.GatewayAdmissionOverride{Concurrency: &groupLimit, WaitMS: &disabled}
	policy, err := ResolveGatewayAdmissionPolicy(config, model.User{Group: "team", GatewayConcurrency: &userLimit})
	require.NoError(t, err)
	assert.Equal(t, 3, policy.UserConcurrency)
	assert.Zero(t, policy.Wait)
	policy, err = ResolveGatewayAdmissionPolicy(config, model.User{Group: "team", GatewayConcurrency: &disabled})
	require.NoError(t, err)
	assert.Zero(t, policy.UserConcurrency)
}

func TestGatewayUserAndChannelLeasesHaveIndependentOwnership(t *testing.T) {
	useRoutePlanningRedis(t)
	ctx := context.Background()
	policy := GatewayAdmissionPolicy{UserConcurrency: 1}
	userLease, err := AcquireGatewayUserLease(ctx, "same-request", 1, policy, nil)
	require.NoError(t, err)
	channelLease, err := AcquireRouteLease(ctx, common.RDB, "same-request", "channel-attempt", GatewayLeaseTTL, []RouteLeaseResource{{Key: ChannelRouteLeaseKey(1), Capacity: 1}})
	require.NoError(t, err)
	assert.NotEqual(t, userLease.RequestID, channelLease.RequestID)
	require.NoError(t, ReleaseRouteLease(ctx, common.RDB, channelLease))
	_, err = AcquireGatewayUserLease(ctx, "second-request", 1, policy, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrRouteLeaseCapacity) || errors.Is(err, ErrGatewayWaitExpired))
	require.NoError(t, ReleaseRouteLease(ctx, common.RDB, userLease))
	userLease, err = AcquireGatewayUserLease(ctx, "second-request", 1, policy, nil)
	require.NoError(t, err)
	require.NoError(t, ReleaseRouteLease(ctx, common.RDB, userLease))
}

func TestGatewayDisabledCapKeepsOccupancyForReenable(t *testing.T) {
	useRoutePlanningRedis(t)
	ctx := context.Background()
	unlimited, err := AcquireGatewayUserLease(ctx, "unlimited", 9, GatewayAdmissionPolicy{}, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, unlimited.LeaseID)
	_, err = AcquireGatewayUserLease(ctx, "enabled", 9, GatewayAdmissionPolicy{UserConcurrency: 1}, nil)
	require.Error(t, err)
	require.NoError(t, ReleaseRouteLease(ctx, common.RDB, unlimited))
	enabled, err := AcquireGatewayUserLease(ctx, "enabled", 9, GatewayAdmissionPolicy{UserConcurrency: 1}, nil)
	require.NoError(t, err)
	require.NoError(t, ReleaseRouteLease(ctx, common.RDB, enabled))
}

func queueTestAttempt(t *testing.T, client *redis.Client, resource RouteLeaseResource, id string, wait time.Duration, queued bool) int {
	t.Helper()
	keys := []string{resource.Key, routeLeaseMetaKey(id), routeLeaseRequestKey(id), resource.Key + ":queue", resource.Key + ":queue_expiry", resource.Key + ":sequence"}
	result, err := gatewayQueuedAcquireScript.Run(context.Background(), client, keys, id, id, GatewayLeaseTTL.Milliseconds(), resource.Capacity, 2, wait.Milliseconds(), queued).Int()
	require.NoError(t, err)
	return result
}

func TestGatewayQueueFIFOAcrossClientsAndHotCapacity(t *testing.T) {
	server := useRoutePlanningRedis(t)
	ctx := context.Background()
	peer := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, peer.Close()) })
	resource := RouteLeaseResource{Key: ChannelRouteLeaseKey(2), Capacity: 1}
	assert.Equal(t, 1, queueTestAttempt(t, common.RDB, resource, "active", time.Second, false))
	assert.Equal(t, 0, queueTestAttempt(t, common.RDB, resource, "first", time.Second, false))
	assert.Equal(t, 0, queueTestAttempt(t, peer, resource, "second", time.Second, false))
	assert.Equal(t, -1, queueTestAttempt(t, peer, resource, "overflow", time.Second, false))
	lease := RouteLease{LeaseID: "active", RequestID: "active", Resources: []RouteLeaseResource{resource}}
	require.NoError(t, ReleaseRouteLease(ctx, common.RDB, lease))
	assert.Equal(t, 0, queueTestAttempt(t, peer, resource, "second", time.Second, true), "later poll must not pass the first ticket")
	assert.Equal(t, 1, queueTestAttempt(t, common.RDB, resource, "first", time.Second, true))
	resource.Capacity = 2
	assert.Equal(t, 1, queueTestAttempt(t, peer, resource, "second", time.Second, true))
	resource.Capacity = 1
	assert.Equal(t, 0, queueTestAttempt(t, common.RDB, resource, "after-decrease", time.Second, false))
	count, err := common.RDB.ZCard(ctx, resource.Key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(2), count, "changing capacity cannot clear active leases")
}

func TestGatewayQueueCancellationAndExpiredTicketRecovery(t *testing.T) {
	server := useRoutePlanningRedis(t)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	server.SetTime(now)
	resource := RouteLeaseResource{Key: UserRouteLeaseKey(3), Capacity: 1}
	assert.Equal(t, 1, queueTestAttempt(t, common.RDB, resource, "active", time.Second, false))
	assert.Equal(t, 0, queueTestAttempt(t, common.RDB, resource, "expired", time.Second, false))
	server.SetTime(now.Add(2 * time.Second))
	assert.Equal(t, -3, queueTestAttempt(t, common.RDB, resource, "expired", time.Second, true))
	assert.Equal(t, 0, queueTestAttempt(t, common.RDB, resource, "next", time.Second, false))
	_, err := gatewayQueueCancelScript.Run(ctx, common.RDB, []string{resource.Key + ":queue", resource.Key + ":queue_expiry"}, "next").Result()
	require.NoError(t, err)
	count, err := common.RDB.ZCard(ctx, resource.Key+":queue").Result()
	require.NoError(t, err)
	assert.Zero(t, count)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = AcquireQueuedRouteLease(canceled, common.RDB, "canceled", "canceled", GatewayLeaseTTL, resource, 2, NewGatewayWaitBudget(time.Second))
	require.ErrorIs(t, err, context.Canceled)
}

func TestGatewayWaitBudgetIsSharedAcrossAdmissionStages(t *testing.T) {
	budget := NewGatewayWaitBudget(5 * time.Second)
	ctx := WithGatewayAdmissionContext(context.Background(), GatewayAdmissionPolicy{}, budget)
	budget.consume(2 * time.Second)
	_, inherited, ok := GatewayAdmissionFromContext(ctx)
	require.True(t, ok)
	assert.Same(t, budget, inherited)
	assert.Equal(t, 3*time.Second, inherited.Remaining())
	inherited.consume(4 * time.Second)
	assert.Zero(t, budget.Remaining())
}

func TestGatewaySuccessReservationIsStrictAcrossClients(t *testing.T) {
	server := useRoutePlanningRedis(t)
	ctx := context.Background()
	peer := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, peer.Close()) })
	var wg sync.WaitGroup
	type result struct {
		reservation GatewayRequestReservation
		err         error
	}
	results := make(chan result, 2)
	for index, client := range []*redis.Client{common.RDB, peer} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, err := ReserveGatewayRequest(ctx, client, "strict", string(rune('a'+index)), time.Minute, 3, 1)
			results <- result{reservation, err}
		}()
	}
	wg.Wait()
	close(results)
	var winner GatewayRequestReservation
	for result := range results {
		if result.err == nil {
			require.Empty(t, winner.ID)
			winner = result.reservation
		} else {
			require.ErrorIs(t, result.err, ErrGatewaySuccessRateLimit)
		}
	}
	require.NotEmpty(t, winner.ID)
	require.NoError(t, FinishGatewayRequestReservation(ctx, common.RDB, winner, false))
	third, err := ReserveGatewayRequest(ctx, peer, "strict", "third", time.Minute, 3, 1)
	require.NoError(t, err)
	require.NoError(t, FinishGatewayRequestReservation(ctx, peer, third, true))
	require.NoError(t, FinishGatewayRequestReservation(ctx, peer, third, true), "finishing success is idempotent")
	_, err = ReserveGatewayRequest(ctx, common.RDB, "strict", "fourth", time.Minute, 3, 1)
	require.ErrorIs(t, err, ErrGatewayTotalRateLimit, "success rejection still counted as a logical request")
}

func TestGatewaySuccessReservationRenewalAndRedisFailure(t *testing.T) {
	server := useRoutePlanningRedis(t)
	ctx := context.Background()
	now := time.Unix(1000, 0)
	server.SetTime(now)
	reservation, err := ReserveGatewayRequest(ctx, common.RDB, "expiry", "first", time.Minute, 0, 1)
	require.NoError(t, err)
	server.SetTime(now.Add(time.Minute))
	require.NoError(t, RenewGatewayRequestReservation(ctx, common.RDB, reservation))
	server.SetTime(now.Add(121 * time.Second))
	_, err = ReserveGatewayRequest(ctx, common.RDB, "expiry", "second", time.Minute, 0, 1)
	require.ErrorIs(t, err, ErrGatewaySuccessRateLimit)
	server.SetTime(now.Add(181 * time.Second))
	require.ErrorIs(t, RenewGatewayRequestReservation(ctx, common.RDB, reservation), ErrGatewayReservationLost)
	_, err = ReserveGatewayRequest(ctx, common.RDB, "expiry", "third", time.Minute, 0, 1)
	require.NoError(t, err)
	common.RedisEnabled = false
	_, err = AcquireGatewayUserLease(ctx, "disabled", 1, GatewayAdmissionPolicy{UserConcurrency: 0}, nil)
	require.ErrorIs(t, err, ErrRouteLeaseUnavailable)
}
