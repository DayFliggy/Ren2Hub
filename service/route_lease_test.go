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
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, PolicyEnabled: true, PolicyVersion: 4},
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 3, CapabilityVersion: 3, PolicyEnabled: true, PolicyVersion: 4},
	)
	assert.ErrorIs(t, err, ErrRouteLeaseRuntime)
	assert.NoError(t, RecheckRouteLeaseRuntime(
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, PolicyEnabled: true, PolicyVersion: 4},
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, PolicyEnabled: true, PolicyVersion: 4},
	))
}

func TestRouteLeaseRuntimeRecheckFencesPolicyState(t *testing.T) {
	expected := RouteLeaseRuntimeState{
		ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3,
		PolicyEnabled: true, PolicyVersion: 4,
	}
	tests := []struct {
		name    string
		current RouteLeaseRuntimeState
	}{
		{name: "disabled", current: RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, PolicyVersion: 4}},
		{name: "version changed", current: RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, PolicyEnabled: true, PolicyVersion: 5}},
		{name: "version missing", current: RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 3, PolicyEnabled: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.ErrorIs(t, RecheckRouteLeaseRuntime(expected, test.current), ErrRouteLeaseRuntime)
		})
	}
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

func TestRouteLeaseTwoRedisClientsCompeteForOneCapacity(t *testing.T) {
	server := miniredis.RunT(t)
	clients := []*redis.Client{
		redis.NewClient(&redis.Options{Addr: server.Addr()}),
		redis.NewClient(&redis.Options{Addr: server.Addr()}),
	}
	for _, client := range clients {
		client := client
		t.Cleanup(func() { _ = client.Close() })
	}
	resources := []RouteLeaseResource{{Key: "route:lease:test:multi-instance", Capacity: 1}}
	start := make(chan struct{})
	type acquireResult struct {
		lease RouteLease
		err   error
	}
	results := make(chan acquireResult, len(clients))
	for index, client := range clients {
		go func(index int, client *redis.Client) {
			<-start
			lease, err := AcquireRouteLease(
				context.Background(), client,
				fmt.Sprintf("request-instance-%d", index), fmt.Sprintf("lease-instance-%d", index),
				time.Minute, resources,
			)
			results <- acquireResult{lease: lease, err: err}
		}(index, client)
	}
	close(start)

	var acquired RouteLease
	successes, capacityErrors := 0, 0
	for range clients {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			acquired = result.lease
		case result.err == ErrRouteLeaseCapacity:
			capacityErrors++
		default:
			require.NoError(t, result.err)
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, capacityErrors)
	require.NoError(t, ReleaseRouteLease(context.Background(), clients[0], acquired))
}

func TestRouteLeaseCapacityFailureDoesNotPartiallyWriteResources(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	userResource := RouteLeaseResource{Key: "route:lease:test:atomic:user", Capacity: 2}
	tokenResource := RouteLeaseResource{Key: "route:lease:test:atomic:token", Capacity: 1}
	channelResource := RouteLeaseResource{Key: "route:lease:test:atomic:channel", Capacity: 2}
	blocker, err := AcquireRouteLease(context.Background(), client, "request-blocker", "lease-blocker", time.Minute, []RouteLeaseResource{tokenResource})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ReleaseRouteLease(context.Background(), client, blocker) })

	_, err = AcquireRouteLease(
		context.Background(), client, "request-atomic", "lease-atomic", time.Minute,
		[]RouteLeaseResource{userResource, tokenResource, channelResource},
	)
	assert.ErrorIs(t, err, ErrRouteLeaseCapacity)
	assert.Equal(t, int64(0), client.ZCard(context.Background(), userResource.Key).Val())
	assert.Equal(t, int64(1), client.ZCard(context.Background(), tokenResource.Key).Val())
	assert.Equal(t, int64(0), client.ZCard(context.Background(), channelResource.Key).Val())
	assert.Equal(t, int64(0), client.Exists(context.Background(), routeLeaseMetaKey("lease-atomic"), routeLeaseRequestKey("request-atomic")).Val())
}

func TestRouteLeaseTTLRecoversAllResourcesAfterProcessCrash(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	resources := []RouteLeaseResource{
		{Key: "route:lease:test:crash:user", Capacity: 1},
		{Key: "route:lease:test:crash:token", Capacity: 1},
		{Key: "route:lease:test:crash:channel", Capacity: 1},
	}
	initialTime := time.Now()
	server.SetTime(initialTime)
	_, err := AcquireRouteLease(context.Background(), client, "request-crashed", "lease-crashed", time.Second, resources)
	require.NoError(t, err)
	server.FastForward(2 * time.Second)
	server.SetTime(initialTime.Add(2 * time.Second))

	recovered, err := AcquireRouteLease(context.Background(), client, "request-recovered", "lease-recovered", time.Minute, resources)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ReleaseRouteLease(context.Background(), client, recovered) })
	for _, resource := range resources {
		assert.Equal(t, []string{"lease-recovered"}, client.ZRange(context.Background(), resource.Key, 0, -1).Val())
	}
	assert.Equal(t, int64(0), client.Exists(context.Background(), routeLeaseMetaKey("lease-crashed"), routeLeaseRequestKey("request-crashed")).Val())
}

func TestRouteLeaseRedisDisconnectFailsClosed(t *testing.T) {
	t.Run("acquire", func(t *testing.T) {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{
			Addr: server.Addr(), MaxRetries: -1,
			DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond,
		})
		t.Cleanup(func() { _ = client.Close() })
		server.Close()
		_, err := AcquireRouteLease(context.Background(), client, "request-acquire", "lease-acquire", time.Minute, []RouteLeaseResource{{Key: "route:lease:test:disconnect:acquire", Capacity: 1}})
		assert.ErrorIs(t, err, ErrRouteLeaseUnavailable)
	})

	t.Run("renew", func(t *testing.T) {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{
			Addr: server.Addr(), MaxRetries: -1,
			DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond,
		})
		t.Cleanup(func() { _ = client.Close() })
		lease, err := AcquireRouteLease(context.Background(), client, "request-renew", "lease-renew", time.Minute, []RouteLeaseResource{{Key: "route:lease:test:disconnect:renew", Capacity: 1}})
		require.NoError(t, err)
		server.Close()
		_, err = RenewRouteLease(context.Background(), client, lease, time.Minute)
		assert.ErrorIs(t, err, ErrRouteLeaseUnavailable)
	})

	t.Run("release", func(t *testing.T) {
		server := miniredis.RunT(t)
		client := redis.NewClient(&redis.Options{
			Addr: server.Addr(), MaxRetries: -1,
			DialTimeout: 100 * time.Millisecond, ReadTimeout: 100 * time.Millisecond, WriteTimeout: 100 * time.Millisecond,
		})
		t.Cleanup(func() { _ = client.Close() })
		lease, err := AcquireRouteLease(context.Background(), client, "request-release", "lease-release", time.Minute, []RouteLeaseResource{{Key: "route:lease:test:disconnect:release", Capacity: 1}})
		require.NoError(t, err)
		server.Close()
		err = ReleaseRouteLease(context.Background(), client, lease)
		assert.ErrorIs(t, err, ErrRouteLeaseUnavailable)
	})
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
