package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalAdmissionCapacityUpdateAndRelease(t *testing.T) {
	m := NewAdmissionManager(10)
	p := AdmissionPolicy{Capacity: 0, QueueSize: 2, Revision: 1}
	a, err := m.TryAcquire(context.Background(), "user:1", p)
	require.NoError(t, err)
	b, err := m.TryAcquire(context.Background(), "user:1", p)
	require.NoError(t, err)
	p.Capacity, p.Revision = 1, 2
	m.ApplyPolicy("user:1", p)
	_, err = m.TryAcquire(context.Background(), "user:1", p)
	require.ErrorIs(t, err, ErrAdmissionCapacity)
	a.Release()
	a.Release()
	assert.Equal(t, int64(1), m.Snapshot("user:1").InFlight)
	_, err = m.TryAcquire(context.Background(), "user:1", p)
	require.ErrorIs(t, err, ErrAdmissionCapacity)
	b.Release()
	c, err := m.TryAcquire(context.Background(), "user:1", p)
	require.NoError(t, err)
	c.Release()
	assert.Empty(t, m.pools)
	assert.Equal(t, m.granted, m.released)
}

func TestLocalAdmissionFIFOAndCancellation(t *testing.T) {
	m := NewAdmissionManager(2)
	p := AdmissionPolicy{Capacity: 1, QueueSize: 2, Revision: 1}
	a, err := m.TryAcquire(context.Background(), "channel:1", p)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() {
		permit, err := m.Acquire(ctx, "channel:1", p, true)
		if permit != nil {
			permit.Release()
		}
		first <- err
	}()
	require.Eventually(t, func() bool { return m.Snapshot("channel:1").Queued == 1 }, time.Second, time.Millisecond)
	second := make(chan *Permit, 1)
	go func() {
		permit, err := m.Acquire(context.Background(), "channel:1", p, true)
		if err != nil {
			second <- nil
			return
		}
		second <- permit
	}()
	require.Eventually(t, func() bool { return m.Snapshot("channel:1").Queued == 2 }, time.Second, time.Millisecond)
	_, err = m.TryAcquire(context.Background(), "channel:1", p)
	require.ErrorIs(t, err, ErrAdmissionCapacity)
	cancel()
	require.ErrorIs(t, <-first, context.Canceled)
	a.Release()
	b := <-second
	require.NotNil(t, b)
	assert.Equal(t, int64(1), m.Snapshot("channel:1").InFlight)
	b.Release()
	assert.Zero(t, m.totalQueued)
	assert.Empty(t, m.pools)
}

func TestLocalAdmissionDrainKeepsChannelAttempts(t *testing.T) {
	m := NewAdmissionManager(0)
	m.BeginDrain()
	p := AdmissionPolicy{Capacity: 1}
	_, err := m.TryAcquire(context.Background(), "user:1", p)
	require.ErrorIs(t, err, ErrGatewayDraining)
	a, err := m.TryAcquire(context.Background(), "channel:1", p)
	require.NoError(t, err)
	defer a.Release()
	_, err = m.Acquire(context.Background(), "channel:1", p, true)
	require.ErrorIs(t, err, ErrGatewayQueueFull)
}

func TestLocalAdmissionExpansionPreservesFIFOAndRejectsLatePolicy(t *testing.T) {
	m := NewAdmissionManager(2)
	key := "user:1"
	policy := AdmissionPolicy{Capacity: 1, QueueSize: 2, Revision: 1}
	first, err := m.TryAcquire(context.Background(), key, policy)
	require.NoError(t, err)
	defer first.Release()
	results := make(chan *Permit, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { permit, _ := m.Acquire(ctx, key, policy, true); results <- permit }()
	require.Eventually(t, func() bool { return m.Snapshot(key).Queued == 1 }, time.Second, time.Millisecond)
	updated := AdmissionPolicy{Capacity: 2, QueueSize: 0, Revision: 2}
	m.ApplyPolicy(key, updated)
	next := <-results
	require.NotNil(t, next, "expansion wakes existing waiter despite the new zero queue limit")
	defer next.Release()
	m.ApplyPolicy(key, AdmissionPolicy{Capacity: 100, QueueSize: 20, Revision: 1})
	_, err = m.TryAcquire(context.Background(), key, policy)
	require.ErrorIs(t, err, ErrAdmissionCapacity)
	assert.Equal(t, 2, m.Snapshot(key).Capacity)
	assert.EqualValues(t, 2, m.Snapshot(key).Revision)
}

func TestLocalAdmissionTotalQueueShrinkAndCancellation(t *testing.T) {
	m := NewAdmissionManager(1)
	policy := AdmissionPolicy{Capacity: 1, QueueSize: 2}
	a, err := m.TryAcquire(context.Background(), "user:1", policy)
	require.NoError(t, err)
	defer a.Release()
	b, err := m.TryAcquire(context.Background(), "channel:2", policy)
	require.NoError(t, err)
	defer b.Release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { permit, err := m.Acquire(ctx, "user:1", policy, true); permit.Release(); done <- err }()
	require.Eventually(t, func() bool { return m.Metrics().Queued == 1 }, time.Second, time.Millisecond)
	_, err = m.Acquire(context.Background(), "channel:2", policy, true)
	require.ErrorIs(t, err, ErrGatewayTotalQueueFull)
	m.SetTotalLimit(0)
	assert.Equal(t, 1, m.Metrics().Queued, "shrinking does not evict registered waiters")
	immediate, err := m.TryAcquire(context.Background(), "channel:3", policy)
	require.NoError(t, err, "total queue limit never rejects available capacity")
	immediate.Release()
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	snapshot := m.Metrics()
	assert.Zero(t, snapshot.Queued)
	assert.EqualValues(t, 1, snapshot.Canceled)
	assert.EqualValues(t, 1, snapshot.TotalQueueFull)
	assert.EqualValues(t, 1, snapshot.WaitBuckets[7])
}

func TestLocalAdmissionCancellationCompetesWithSlotTransfer(t *testing.T) {
	m := NewAdmissionManager(1)
	policy := AdmissionPolicy{Capacity: 1, QueueSize: 1}
	first, err := m.TryAcquire(context.Background(), "user:1", policy)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { permit, err := m.Acquire(ctx, "user:1", policy, true); permit.Release(); done <- err }()
	require.Eventually(t, func() bool { return m.Metrics().Queued == 1 }, time.Second, time.Millisecond)
	start, released := make(chan struct{}), make(chan struct{})
	go func() { <-start; first.Release(); close(released) }()
	close(start)
	cancel()
	err = <-done
	require.True(t, err == nil || errors.Is(err, context.Canceled))
	<-released
	snapshot := m.Metrics()
	assert.Zero(t, snapshot.InFlight)
	assert.Zero(t, snapshot.Queued)
	assert.Zero(t, snapshot.Pools)
	assert.Equal(t, snapshot.Granted, snapshot.Released)
}

func TestLocalAdmissionDrainRejectsQueuedUsers(t *testing.T) {
	m := NewAdmissionManager(1)
	policy := AdmissionPolicy{Capacity: 1, QueueSize: 1}
	first, err := m.TryAcquire(context.Background(), "user:1", policy)
	require.NoError(t, err)
	defer first.Release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { permit, err := m.Acquire(ctx, "user:1", policy, true); permit.Release(); done <- err }()
	require.Eventually(t, func() bool { return m.Metrics().Queued == 1 }, time.Second, time.Millisecond)
	m.BeginDrain()
	require.ErrorIs(t, <-done, ErrGatewayDraining)
	assert.Zero(t, m.Metrics().Queued)
	assert.EqualValues(t, 1, m.Metrics().InFlight)
}
