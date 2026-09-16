package service

import (
	"context"
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
	permit, err := AcquireGatewayUserPermit(ctx, "disabled", 1, GatewayAdmissionPolicy{UserConcurrency: 0, Revision: admissionRevision.Load()}, nil)
	require.NoError(t, err)
	permit.Release()
}
