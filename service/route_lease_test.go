package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteLeaseAcquireReleaseRenewAndOwnership(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	resources := []RouteLeaseResource{{Key: "route:lease:test:channel", Capacity: 1}}

	lease, err := AcquireRouteLease(context.Background(), client, "request-1", "lease-1", time.Minute, resources)
	require.NoError(t, err)
	_, err = AcquireRouteLease(context.Background(), client, "request-2", "lease-2", time.Minute, resources)
	assert.ErrorIs(t, err, ErrRouteLeaseCapacity)
	_, err = AcquireRouteLease(context.Background(), client, "request-1", "lease-3", time.Minute, []RouteLeaseResource{{Key: "route:lease:test:other", Capacity: 1}})
	assert.ErrorIs(t, err, ErrRouteLeaseConflict)

	_, err = RenewRouteLease(context.Background(), client, RouteLease{LeaseID: "lease-1", RequestID: "wrong", Resources: resources}, time.Minute)
	assert.ErrorIs(t, err, ErrRouteLeaseOwnership)
	_, err = RenewRouteLease(context.Background(), client, lease, time.Minute)
	require.NoError(t, err)

	err = ReleaseRouteLease(context.Background(), client, RouteLease{LeaseID: "lease-1", RequestID: "wrong", Resources: resources})
	assert.ErrorIs(t, err, ErrRouteLeaseOwnership)
	require.NoError(t, ReleaseRouteLease(context.Background(), client, lease))
	_, err = AcquireRouteLease(context.Background(), client, "request-2", "lease-2", time.Minute, resources)
	require.NoError(t, err)
}

func TestRouteLeaseExpiresAndRuntimeRecheckFailsClosed(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	resources := []RouteLeaseResource{{Key: "route:lease:test:channel", Capacity: 1}}
	now := time.Now()
	server.SetTime(now)
	_, err := AcquireRouteLease(context.Background(), client, "request-1", "lease-1", time.Second, resources)
	require.NoError(t, err)
	server.SetTime(now.Add(2 * time.Second))
	_, err = AcquireRouteLease(context.Background(), client, "request-2", "lease-2", time.Minute, resources)
	require.NoError(t, err)

	err = RecheckRouteLeaseRuntime(
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3},
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 3, CapabilityVersion: 3},
	)
	assert.ErrorIs(t, err, ErrRouteLeaseRuntime)
	assert.NoError(t, RecheckRouteLeaseRuntime(
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3},
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3},
	))
}
