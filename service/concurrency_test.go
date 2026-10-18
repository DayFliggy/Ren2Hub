package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
)

func TestConcurrencyUnlimitedHotUpdateAndOnceRelease(t *testing.T) {
	m := NewConcurrencyManager()
	ctx := context.Background()
	first, err := m.tryAcquire(ctx, "user", 1, 0)
	require.NoError(t, err)
	second, err := m.tryAcquire(ctx, "user", 1, 0)
	require.NoError(t, err)
	m.setLimit("user", 1, 1, false)
	_, err = m.tryAcquire(ctx, "user", 1, 0)
	require.ErrorIs(t, err, ErrConcurrencyFull)
	first.Release()
	first.Release()
	_, err = m.tryAcquire(ctx, "user", 1, 0)
	require.ErrorIs(t, err, ErrConcurrencyFull)
	second.Release()
	next, err := m.tryAcquire(ctx, "user", 1, 0)
	require.NoError(t, err)
	m.setLimit("user", 1, 0, false)
	other, err := m.tryAcquire(ctx, "user", 1, 1)
	require.NoError(t, err)
	next.Release()
	other.Release()
	require.Zero(t, m.snapshot("user")[0].InFlight)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = m.tryAcquire(canceled, "user", 1, 0)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, m.snapshot("user")[0].InFlight)
}
func TestConcurrencyParallelNeverExceedsLimit(t *testing.T) {
	m := NewConcurrencyManager()
	var active, peak atomic.Int64
	var wg, attempted sync.WaitGroup
	attempted.Add(100)
	start := make(chan struct{})
	release := make(chan struct{})
	for range 100 {
		wg.Go(func() {
			<-start
			p, err := m.tryAcquire(context.Background(), "channel", 1, 3)
			attempted.Done()
			if err != nil {
				if !errors.Is(err, ErrConcurrencyFull) {
					t.Error(err)
				}
				return
			}
			n := active.Add(1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			<-release
			active.Add(-1)
			p.Release()
		})
	}
	close(start)
	attempted.Wait()
	close(release)
	wg.Wait()
	require.Equal(t, int64(3), peak.Load())
	require.Zero(t, m.snapshot("channel")[0].InFlight)
}
func TestBusyManualChannelScansNextAndAllBusyReturns429(t *testing.T) {
	db := setupRouteTest(t)
	for _, id := range []int{11, 22} {
		createRouteChannel(t, db, id, "route-model")
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Update("max_concurrency", 1).Error)
	}
	saveRouteTest(t, 11, 22)
	c1, c2, c3 := routeTestContext(t), routeTestContext(t), routeTestContext(t)
	first, err := selectQualifiedChannel(c1, "route-model")
	require.NoError(t, err)
	require.Equal(t, 11, first.Id)
	second, err := selectQualifiedChannel(c2, "route-model")
	require.NoError(t, err)
	require.Equal(t, 22, second.Id)
	_, full := SelectChannelForRequest(c3, "route-model", nil)
	require.NotNil(t, full)
	require.Equal(t, 429, full.StatusCode)
	ReleaseRouteChannel(c1)
	ReleaseRouteChannel(c1)
	require.Zero(t, ChannelInFlight(11))
	next, err := selectQualifiedChannel(c3, "route-model")
	require.NoError(t, err)
	require.Equal(t, 11, next.Id)
}
func TestAutomaticPriorityUnknownAndZeroWeightBackup(t *testing.T) {
	previous := routeObservations.rows
	routeObservations.rows = map[routeObservationKey]RouteObservationSnapshot{}
	t.Cleanup(func() { routeObservations.rows = previous })
	rows := []*model.Channel{{Id: 1, Priority: common.GetPointer(int64(0)), Weight: common.GetPointer(uint(100))}, {Id: 2, Priority: common.GetPointer(int64(10)), Weight: common.GetPointer(uint(0))}, {Id: 3, Priority: common.GetPointer(int64(10)), Weight: common.GetPointer(uint(100))}}
	orderAutomaticRoutes(rows, "request", "model", 1, false)
	require.Equal(t, 3, rows[0].Id)
	require.Equal(t, 2, rows[1].Id)
	// Unknown channels occupy the normal tier and eventually receive weighted tie opportunities.
	winners := map[int]bool{}
	equal := []*model.Channel{{Id: 5, Weight: common.GetPointer(uint(1))}, {Id: 6, Weight: common.GetPointer(uint(1))}}
	for i := range 100 {
		orderAutomaticRoutes(equal, string(rune(i)), "model", 1, false)
		winners[equal[0].Id] = true
	}
	require.Len(t, winners, 2)
	require.Equal(t, 0, routeLoadBucket(0))
	require.Equal(t, 1, routeLoadBucket(1))
	require.Equal(t, 2, routeLoadBucket(4))
}

func TestConcurrencyRefreshUsesCommittedValueAndKeepsBorrowers(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "model")
	ch := &model.Channel{Id: 11, MaxConcurrency: 0}
	p, err := AcquireChannelConcurrency(context.Background(), ch)
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("max_concurrency", 1).Error)
	require.NoError(t, RefreshChannelConcurrencyLimit(context.Background(), 11))
	_, err = AcquireChannelConcurrency(context.Background(), ch)
	require.ErrorIs(t, err, ErrConcurrencyFull)
	require.Equal(t, 1, ChannelInFlight(11))
	p.Release()
	require.Zero(t, ChannelInFlight(11))
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("max_concurrency", 0).Error)
	require.NoError(t, RefreshChannelConcurrencyLimit(context.Background(), 11))
	first, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11, MaxConcurrency: 1})
	require.NoError(t, err)
	defer first.Release()
	second, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11, MaxConcurrency: 1})
	require.NoError(t, err)
	defer second.Release()
}
