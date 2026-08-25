package service

import (
	"context"
	"fmt"
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
	_, err = RenewRouteLease(context.Background(), client, RouteLease{
		LeaseID: "lease-1", RequestID: "request-1", Resources: []RouteLeaseResource{{Key: "route:lease:test:wrong", Capacity: 1}},
	}, time.Minute)
	assert.ErrorIs(t, err, ErrRouteLeaseOwnership)

	err = ReleaseRouteLease(context.Background(), client, RouteLease{LeaseID: "lease-1", RequestID: "wrong", Resources: resources})
	assert.ErrorIs(t, err, ErrRouteLeaseOwnership)
	err = ReleaseRouteLease(context.Background(), client, RouteLease{
		LeaseID: "lease-1", RequestID: "request-1", Resources: []RouteLeaseResource{{Key: "route:lease:test:wrong", Capacity: 1}},
	})
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

func TestRouteLeaseConcurrentAcquisitionHonorsCapacity(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	resources := []RouteLeaseResource{{Key: "route:lease:test:concurrent", Capacity: 3}}

	const attempts = 4
	start := make(chan struct{})
	type acquireResult struct {
		lease RouteLease
		err   error
	}
	results := make(chan acquireResult, attempts)

	for index := 0; index < attempts; index++ {
		go func(index int) {
			<-start
			lease, err := AcquireRouteLease(
				context.Background(), client,
				fmt.Sprintf("request-%d", index), fmt.Sprintf("lease-%d", index),
				time.Minute, resources,
			)
			results <- acquireResult{lease: lease, err: err}
		}(index)
	}
	close(start)

	leases := make([]RouteLease, 0, 3)
	capacityErrors := 0
	otherErrors := make([]error, 0)
	for index := 0; index < attempts; index++ {
		result := <-results
		if result.err == nil {
			leases = append(leases, result.lease)
			continue
		}
		if result.err == ErrRouteLeaseCapacity {
			capacityErrors++
			continue
		}
		otherErrors = append(otherErrors, result.err)
	}

	assert.Len(t, leases, 3)
	assert.Equal(t, attempts-3, capacityErrors)
	assert.Empty(t, otherErrors)
	for _, lease := range leases {
		require.NoError(t, ReleaseRouteLease(context.Background(), client, lease))
	}
}

func TestRouteLeaseRenewExtendsLeaseAndRenewalReportsUnavailableRedis(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	resources := []RouteLeaseResource{{Key: "route:lease:test:renewal", Capacity: 1}}
	now := time.Now()
	server.SetTime(now)
	lease, err := AcquireRouteLease(context.Background(), client, "request-renew", "lease-renew", time.Second, resources)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ReleaseRouteLease(context.Background(), client, lease)
	})
	server.FastForward(900 * time.Millisecond)
	_, err = RenewRouteLease(context.Background(), client, lease, time.Second)
	require.NoError(t, err)
	server.FastForward(200 * time.Millisecond)
	_, err = AcquireRouteLease(context.Background(), client, "request-blocked", "lease-blocked", time.Minute, resources)
	assert.ErrorIs(t, err, ErrRouteLeaseCapacity)

	failedRenewal := StartRouteLeaseRenewal(context.Background(), nil, lease, time.Millisecond, time.Minute)
	renewErr, ok := <-failedRenewal.Done
	require.True(t, ok)
	assert.ErrorIs(t, renewErr, ErrRouteLeaseUnavailable)
}
