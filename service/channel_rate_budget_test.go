package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withChannelRateRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = previousRDB, previousEnabled
		_ = client.Close()
	})
	return client
}

// A zero budget means unlimited, so no window state is created and nothing is
// charged. This is the default for every existing channel.
func TestChannelRateBudgetZeroMeansUnlimited(t *testing.T) {
	withChannelRateRedis(t)
	for range 3 {
		reservation, err := ReserveChannelRateBudget(context.Background(), 41, ChannelRateBudget{}, 0)
		require.NoError(t, err)
		assert.False(t, reservation.RPMCounted)
		assert.Zero(t, reservation.ReservedTokens)
	}
}

func TestChannelRateBudgetEnforcesRPMWithinWindow(t *testing.T) {
	withChannelRateRedis(t)
	budget := ChannelRateBudget{RPM: 2}

	first, err := ReserveChannelRateBudget(context.Background(), 42, budget, 0)
	require.NoError(t, err)
	assert.True(t, first.RPMCounted)
	_, err = ReserveChannelRateBudget(context.Background(), 42, budget, 0)
	require.NoError(t, err)
	_, err = ReserveChannelRateBudget(context.Background(), 42, budget, 0)
	assert.ErrorIs(t, err, ErrChannelRPM)

	// An attempt that never sent returns its slot, so the refused caller fits.
	require.NoError(t, ReleaseChannelRateBudget(context.Background(), first, false))
	_, err = ReserveChannelRateBudget(context.Background(), 42, budget, 0)
	require.NoError(t, err)
}

func TestChannelRateBudgetEnforcesTPMAndCorrectsAgainstUsage(t *testing.T) {
	withChannelRateRedis(t)
	budget := ChannelRateBudget{TPM: 1000}

	reservation, err := ReserveChannelRateBudget(context.Background(), 43, budget, 800)
	require.NoError(t, err)
	assert.Equal(t, 800, reservation.ReservedTokens)

	// 800 of 1000 are held, so a second 800-token ceiling cannot fit.
	_, err = ReserveChannelRateBudget(context.Background(), 43, budget, 800)
	assert.ErrorIs(t, err, ErrChannelTPM)

	// Correcting to real usage frees the over-reserved remainder.
	require.NoError(t, SettleChannelRateBudget(context.Background(), reservation, 100))
	_, err = ReserveChannelRateBudget(context.Background(), 43, budget, 800)
	require.NoError(t, err)
}

// A request whose token ceiling is unknown, or which alone exceeds the window,
// must not enter a TPM-enabled candidate: reserving a guessed amount would
// either overcount every other caller or let this one through unmetered.
func TestChannelRateBudgetRefusesUnboundedTokenCeiling(t *testing.T) {
	withChannelRateRedis(t)
	budget := ChannelRateBudget{TPM: 500}

	for _, ceiling := range []int{0, -1, 501} {
		_, err := ReserveChannelRateBudget(context.Background(), 44, budget, ceiling)
		assert.ErrorIs(t, err, ErrChannelTPMUnbounded)
	}
	// The refusals charged nothing, so a bounded request still fits the window.
	_, err := ReserveChannelRateBudget(context.Background(), 44, budget, 500)
	require.NoError(t, err)
}

// An enabled hard limit has no shared window without Redis. Degrading to a
// per-node counter would multiply the effective limit by the instance count,
// so the request is refused instead.
func TestChannelRateBudgetRefusesWhenRedisUnavailable(t *testing.T) {
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = nil, false
	t.Cleanup(func() { common.RDB, common.RedisEnabled = previousRDB, previousEnabled })

	_, err := ReserveChannelRateBudget(context.Background(), 45, ChannelRateBudget{RPM: 10}, 10)
	assert.ErrorIs(t, err, ErrRouteLeaseUnavailable)

	// An unlimited channel still needs no Redis at all.
	_, err = ReserveChannelRateBudget(context.Background(), 45, ChannelRateBudget{}, 0)
	assert.NoError(t, err)
}

// A rejected reservation must leave no partial charge: RPM is only incremented
// once TPM has also been checked.
func TestChannelRateBudgetRejectionLeavesNoPartialCharge(t *testing.T) {
	withChannelRateRedis(t)
	budget := ChannelRateBudget{RPM: 5, TPM: 100}

	_, err := ReserveChannelRateBudget(context.Background(), 46, budget, 101)
	assert.ErrorIs(t, err, ErrChannelTPMUnbounded)
	full, err := ReserveChannelRateBudget(context.Background(), 46, budget, 100)
	require.NoError(t, err)

	// The TPM window is now full while RPM has been charged exactly once.
	_, err = ReserveChannelRateBudget(context.Background(), 46, budget, 1)
	assert.ErrorIs(t, err, ErrChannelTPM)
	require.NoError(t, SettleChannelRateBudget(context.Background(), full, 0))
	for range 4 {
		_, err = ReserveChannelRateBudget(context.Background(), 46, budget, 1)
		require.NoError(t, err)
	}
	_, err = ReserveChannelRateBudget(context.Background(), 46, budget, 1)
	assert.ErrorIs(t, err, ErrChannelRPM)
}
