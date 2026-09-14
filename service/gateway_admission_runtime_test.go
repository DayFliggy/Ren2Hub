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

func TestGatewayRuntimeIgnoresExpiredOccupancyWithoutChangingOwnership(t *testing.T) {
	server := useRoutePlanningRedis(t)
	now := time.Unix(2000, 0)
	server.SetTime(now)
	ctx := context.Background()
	resource := RouteLeaseResource{Key: UserRouteLeaseKey(2), Capacity: 1}
	assert.Equal(t, 1, queueTestAttempt(t, common.RDB, resource, "running", time.Second, false))
	assert.Equal(t, 0, queueTestAttempt(t, common.RDB, resource, "waiting", time.Second, false))
	before, err := ReadGatewayResourceRuntime(ctx, common.RDB, resource.Key)
	require.NoError(t, err)
	assert.Equal(t, GatewayResourceRuntime{InFlight: 1, Queued: 1}, before)
	server.SetTime(now.Add(2 * time.Second))
	after, err := ReadGatewayResourceRuntime(ctx, common.RDB, resource.Key)
	require.NoError(t, err)
	assert.Equal(t, GatewayResourceRuntime{InFlight: 1, Queued: 0}, after)
	assert.Equal(t, int64(1), common.RDB.ZCard(ctx, resource.Key+":queue").Val())
	_, err = RenewRouteLease(ctx, common.RDB, RouteLease{LeaseID: "running", RequestID: "running", Resources: []RouteLeaseResource{resource}}, GatewayLeaseTTL)
	require.NoError(t, err)
}

func TestGatewayRuntimeReportsSharedProviderUsageAndExpiry(t *testing.T) {
	server := useRoutePlanningRedis(t)
	now := time.Unix(2000, 0)
	server.SetTime(now)
	ctx := context.Background()
	peer := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { require.NoError(t, peer.Close()) })
	scopes := []GatewayProviderRateScope{{ID: GatewayChannelRateScopeID(7), TPM: 100}}
	first, err := ReserveGatewayProviderRate(ctx, common.RDB, scopes, 40)
	require.NoError(t, err)
	_, err = ReserveGatewayProviderRate(ctx, peer, scopes, 30)
	require.NoError(t, err)
	require.NoError(t, FinishGatewayProviderRate(ctx, common.RDB, first, 25, true, true, 10*time.Second))
	runtime, err := ReadGatewayProviderRateRuntime(ctx, peer, scopes[0].ID)
	require.NoError(t, err)
	assert.Equal(t, GatewayProviderRateRuntime{RPMUsed: 2, TokensUsed: 25, TokensReserved: 30, CooldownMS: 10000}, runtime)
	server.SetTime(now.Add(61 * time.Second))
	runtime, err = ReadGatewayProviderRateRuntime(ctx, peer, scopes[0].ID)
	require.NoError(t, err)
	assert.Equal(t, GatewayProviderRateRuntime{TokensReserved: 30}, runtime)
}
