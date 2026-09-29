package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayProviderRateAtomicScopesAndRefundBeforeSend(t *testing.T) {
	server := useRoutePlanningRedis(t)
	ctx := context.Background()
	peer := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, peer.Close()) })
	scopes := []GatewayProviderRateScope{
		{ID: GatewayChannelRateScopeID(1), RPM: 5, TPM: 100},
		{ID: GatewayCredentialRateScopeID("stable-credential"), RPM: 1, TPM: 100},
	}
	first, err := ReserveGatewayProviderRate(ctx, common.RDB, scopes, 60)
	require.NoError(t, err)
	_, err = ReserveGatewayProviderRate(ctx, peer, scopes, 20)
	require.ErrorIs(t, err, ErrGatewayProviderRateLimit)
	keys := gatewayProviderRateKeys(first)
	assert.Equal(t, int64(1), common.RDB.ZCard(ctx, keys[1]).Val(), "failed shared credential admission must not partly consume channel RPM")
	require.NoError(t, FinishGatewayProviderRate(ctx, common.RDB, first, 0, false, false, 0))
	second, err := ReserveGatewayProviderRate(ctx, peer, scopes, 90)
	require.NoError(t, err)
	require.NoError(t, FinishGatewayProviderRate(ctx, peer, second, 20, true, true, 0))
	require.NoError(t, FinishGatewayProviderRate(ctx, peer, second, 20, true, true, 0))
	assert.Equal(t, "20", common.RDB.HGet(ctx, keys[0], second.ID).Val())
	_, err = ReserveGatewayProviderRate(ctx, common.RDB, scopes, 1)
	require.ErrorIs(t, err, ErrGatewayProviderRateLimit, "actual sent attempt retains its RPM")
}

func TestGatewayProviderTPMIncludesPendingAndCorrectsFinalUsage(t *testing.T) {
	server := useRoutePlanningRedis(t)
	server.SetTime(time.Unix(1200, 0))
	ctx := context.Background()
	scope := []GatewayProviderRateScope{{ID: "credential:shared", TPM: 100}}
	first, err := ReserveGatewayProviderRate(ctx, common.RDB, scope, 80)
	require.NoError(t, err)
	_, err = ReserveGatewayProviderRate(ctx, common.RDB, scope, 30)
	require.ErrorIs(t, err, ErrGatewayProviderRateLimit)
	require.NoError(t, FinishGatewayProviderRate(ctx, common.RDB, first, 40, true, true, 0))
	second, err := ReserveGatewayProviderRate(ctx, common.RDB, scope, 60)
	require.NoError(t, err)
	require.NoError(t, FinishGatewayProviderRate(ctx, common.RDB, second, 0, true, false, 0))
	_, err = ReserveGatewayProviderRate(ctx, common.RDB, scope, 1)
	require.ErrorIs(t, err, ErrGatewayProviderRateLimit, "unknown usage retains its estimate")
	server.SetTime(time.Unix(1261, 0))
	_, err = ReserveGatewayProviderRate(ctx, common.RDB, scope, 100)
	require.NoError(t, err)
}

func TestGatewayProviderRenewAndSharedRetryAfter(t *testing.T) {
	server := useRoutePlanningRedis(t)
	ctx := context.Background()
	now := time.Unix(1200, 0)
	server.SetTime(now)
	scope := []GatewayProviderRateScope{{ID: "credential:cooldown", TPM: 100}}
	first, err := ReserveGatewayProviderRate(ctx, common.RDB, scope, 10)
	require.NoError(t, err)
	second, err := ReserveGatewayProviderRate(ctx, common.RDB, scope, 10)
	require.NoError(t, err)
	server.SetTime(now.Add(time.Minute))
	require.NoError(t, RenewGatewayProviderRate(ctx, common.RDB, first))
	require.NoError(t, FinishGatewayProviderRate(ctx, common.RDB, first, 1, true, true, 2*time.Minute))
	require.NoError(t, FinishGatewayProviderRate(ctx, common.RDB, second, 1, true, true, time.Second))
	server.SetTime(now.Add(62 * time.Second))
	_, err = ReserveGatewayProviderRate(ctx, common.RDB, scope, 1)
	require.ErrorIs(t, err, ErrGatewayProviderRateLimit, "shorter failure must not shorten shared Retry-After")
	server.SetTime(now.Add(181 * time.Second))
	_, err = ReserveGatewayProviderRate(ctx, common.RDB, scope, 1)
	require.NoError(t, err)
}

func TestGatewayProviderRateMemoryFallback(t *testing.T) {
	ctx := context.Background()
	scope := []GatewayProviderRateScope{{ID: "memory-test:" + t.Name(), RPM: 1, TPM: 100}}
	first, err := ReserveGatewayProviderRate(ctx, nil, scope, 80)
	require.NoError(t, err)
	_, err = ReserveGatewayProviderRate(ctx, nil, scope, 1)
	require.ErrorIs(t, err, ErrGatewayProviderRateLimit)
	require.NoError(t, FinishGatewayProviderRate(ctx, nil, first, 40, true, true, 0))
	require.NoError(t, FinishGatewayProviderRate(ctx, nil, first, 40, true, true, 0))
	_, err = ReserveGatewayProviderRate(ctx, nil, scope, 1)
	require.ErrorIs(t, err, ErrGatewayProviderRateLimit, "a sent request must retain its RPM")
}
