package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

func TestConcurrencySlowPublicationDoesNotBlockUnrelatedAdmission(t *testing.T) {
	db := setupRouteTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "slow-publication", AffCode: "slow-publication", MaxConcurrency: 1}).Error)
	createRouteChannel(t, db, 22, "route-model")
	started, unblock, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:slow-concurrency-publication", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			close(started)
			<-unblock
		}
	}))
	go func() { finished <- RefreshUserConcurrencyLimit(context.Background(), 17) }()
	defer func() {
		close(unblock)
		require.NoError(t, <-finished)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("publication did not reach its database read")
	}
	admitted := make(chan error, 1)
	go func() {
		permit, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 22})
		if permit != nil {
			permit.Release()
		}
		admitted <- err
	}()
	select {
	case err := <-admitted:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("unrelated admission waited for configuration database I/O")
	}
}

func TestConcurrencyPendingPublicationCannotAdmitWithStaleLimit(t *testing.T) {
	for _, kind := range []string{"user", "channel"} {
		t.Run(kind, func(t *testing.T) {
			db := setupRouteTest(t)
			require.NoError(t, db.AutoMigrate(&model.User{}))
			require.NoError(t, db.Create(&model.User{Id: 17, Username: "pending-limit", AffCode: "pending-limit"}).Error)
			createRouteChannel(t, db, 17, "route-model")
			key := concurrencyKey{kind, 17}
			first, err := acquireConcurrency(context.Background(), key, 0)
			require.NoError(t, err)
			defer first.Release()
			// Simulate admission having observed no pending publication just
			// before the administrator commits a lower limit.
			require.NoError(t, refreshPendingConcurrencyLimit(context.Background(), key))
			var target any = &model.User{}
			table := "users"
			refresh := RefreshUserConcurrencyLimit
			if kind == "channel" {
				target, table, refresh = &model.Channel{}, "channels", RefreshChannelConcurrencyLimit
			}
			require.NoError(t, db.Model(target).Where("id = ?", 17).Update("max_concurrency", 1).Error)
			started, unblock, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:block-pending-publication", func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					close(started)
					<-unblock
				}
			}))
			go func() { finished <- refresh(context.Background(), 17) }()
			var release sync.Once
			unblockPublication := func() { release.Do(func() { close(unblock) }) }
			defer func() {
				unblockPublication()
				require.NoError(t, <-finished)
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("publication did not reach its database read")
			}
			_, err = gatewayConcurrency.tryAcquire(context.Background(), kind, 17, 0)
			require.ErrorIs(t, err, errConcurrencyPublicationPending)
			require.Equal(t, 1, gatewayConcurrency.snapshot(kind)[0].InFlight)
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = acquireConcurrency(cancelled, key, 0)
			require.ErrorIs(t, err, context.Canceled)
			unblockPublication()
			// The publishing goroutine still owns the publication lock until
			// the limit is visible; admission rechecks it before incrementing.
			_, err = acquireConcurrency(context.Background(), key, 0)
			require.ErrorIs(t, err, ErrConcurrencyFull)
			require.Equal(t, ConcurrencySnapshot{ID: 17, Limit: 1, InFlight: 1}, gatewayConcurrency.snapshot(kind)[0])
		})
	}
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

func TestConcurrencyCommittedPublicationSurvivesCallerCancellation(t *testing.T) {
	for _, kind := range []string{"user", "channel"} {
		t.Run(kind, func(t *testing.T) {
			db := setupRouteTest(t)
			require.NoError(t, db.AutoMigrate(&model.User{}))
			require.NoError(t, db.Create(&model.User{Id: 17, Username: "concurrency-user", AffCode: "concurrency-user"}).Error)
			createRouteChannel(t, db, 17, "route-model")
			var target any = &model.User{}
			acquire := func() (*ConcurrencyPermit, error) { return AcquireUserConcurrency(context.Background(), 17, 0) }
			refresh := RefreshUserConcurrencyLimit
			if kind == "channel" {
				target = &model.Channel{}
				acquire = func() (*ConcurrencyPermit, error) {
					return AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 17, MaxConcurrency: 0})
				}
				refresh = RefreshChannelConcurrencyLimit
			}
			first, err := acquire()
			require.NoError(t, err)
			defer first.Release()
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			for _, limit := range []int{1, 2, 0, 1} {
				require.NoError(t, db.Model(target).Where("id = ?", 17).Update("max_concurrency", limit).Error)
				require.NoError(t, refresh(cancelled, 17))
				snapshot := gatewayConcurrency.snapshot(kind)
				require.Equal(t, limit, snapshot[0].Limit)
				require.Equal(t, 1, snapshot[0].InFlight)
				next, err := acquire() // Deliberately stale request/cache value is zero.
				if limit == 1 {
					require.ErrorIs(t, err, ErrConcurrencyFull)
				} else {
					require.NoError(t, err)
					next.Release()
				}
			}
			first.Release()
			require.Zero(t, gatewayConcurrency.snapshot(kind)[0].InFlight)
		})
	}
}

func TestConcurrencyFailedPublicationRepairsBeforeAdmission(t *testing.T) {
	for _, kind := range []string{"user", "channel"} {
		t.Run(kind, func(t *testing.T) {
			db := setupRouteTest(t)
			require.NoError(t, db.AutoMigrate(&model.User{}))
			require.NoError(t, db.Create(&model.User{Id: 17, Username: "publication-user", AffCode: "publication-user"}).Error)
			createRouteChannel(t, db, 17, "route-model")
			var target any = &model.User{}
			table := "users"
			acquire := func() (*ConcurrencyPermit, error) { return AcquireUserConcurrency(context.Background(), 17, 0) }
			refresh := RefreshUserConcurrencyLimit
			if kind == "channel" {
				target, table = &model.Channel{}, "channels"
				acquire = func() (*ConcurrencyPermit, error) {
					return AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 17})
				}
				refresh = RefreshChannelConcurrencyLimit
			}
			first, err := acquire()
			require.NoError(t, err)
			defer first.Release()
			require.NoError(t, db.Model(target).Where("id = ?", 17).Update("max_concurrency", 1).Error)
			failure := errors.New("publication read unavailable")
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("concurrency:publication-fail", func(tx *gorm.DB) {
				if tx.Statement.Table == table {
					tx.AddError(failure)
				}
			}))
			require.ErrorIs(t, refresh(context.Background(), 17), failure)
			_, err = acquire()
			require.ErrorIs(t, err, failure, "unconfirmed limit must not admit with the old unlimited value")
			require.Equal(t, 1, gatewayConcurrency.snapshot(kind)[0].InFlight)
			require.NoError(t, db.Callback().Query().Remove("concurrency:publication-fail"))
			_, err = acquire()
			require.ErrorIs(t, err, ErrConcurrencyFull)
			require.Equal(t, 1, gatewayConcurrency.snapshot(kind)[0].Limit)
			first.Release()
			next, err := acquire()
			require.NoError(t, err)
			next.Release()
			require.Zero(t, gatewayConcurrency.snapshot(kind)[0].InFlight)
		})
	}
}

func TestChannelUpdatePublishesConcurrencyBeforePostCommitReadFailure(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	permit, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11})
	require.NoError(t, err)
	defer permit.Release()
	published := false
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:observe-channel-concurrency-publication", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" && len(tx.Statement.Selects) == 2 {
			published = true
		}
	}))
	failure := errors.New("post-commit channel read unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-channel-post-commit-read", func(tx *gorm.DB) {
		if published && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" && len(tx.Statement.Selects) != 2 {
			tx.AddError(failure)
		}
	}))
	channel := model.Channel{Id: 11, ChannelRatio: "1", MaxConcurrency: 1}
	require.ErrorIs(t, channel.Update(), failure)
	require.True(t, published)
	_, err = AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11})
	require.ErrorIs(t, err, ErrConcurrencyFull)
	require.Equal(t, ConcurrencySnapshot{ID: 11, Limit: 1, InFlight: 1}, gatewayConcurrency.snapshot("channel")[0])
}

func TestChannelUpdatePublicationFailureStillUpdatesAbilities(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "previous-model")
	failure := errors.New("channel concurrency lookup unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-channel-limit-publication", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" && len(tx.Statement.Selects) == 2 {
			tx.AddError(failure)
		}
	}))
	channel := model.Channel{Id: 11, Models: "current-model", ChannelRatio: "1", MaxConcurrency: 1}
	require.ErrorIs(t, channel.Update(), failure)
	var abilities []model.Ability
	require.NoError(t, db.Where("channel_id = ?", 11).Find(&abilities).Error)
	require.Len(t, abilities, 1)
	require.Equal(t, "current-model", abilities[0].Model)
	_, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11})
	require.ErrorIs(t, err, failure, "failed publication must keep admission closed until it can read the committed limit")
	require.NoError(t, db.Callback().Query().Remove("test:fail-channel-limit-publication"))
	permit, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11})
	require.NoError(t, err)
	permit.Release()
	require.Equal(t, ConcurrencySnapshot{ID: 11, Limit: 1, InFlight: 0}, gatewayConcurrency.snapshot("channel")[0])
}

// 删除仍有在途请求的渠道后再用同一 ID 新建：两者共用一份计数并按新渠道的上限准入，
// 删除前发出的尝试也不能把失败、熔断或冷却写进新渠道。
func TestDeletedChannelKeepsInFlightCountAndObservationsForReusedID(t *testing.T) {
	setupRouteTest(t)
	resetRouteObservations(t)
	ctx := context.Background()
	routeObservations.mu.Lock()
	staleEpoch := routeObservations.epoch
	routeObservations.mu.Unlock()
	held, err := AcquireChannelConcurrency(ctx, &model.Channel{Id: 11, MaxConcurrency: 1})
	require.NoError(t, err)

	ReleaseDeletedChannels(ctx, []int{11})
	recreated := &model.Channel{Id: 11, MaxConcurrency: 2}
	reused, err := AcquireChannelConcurrency(ctx, recreated)
	require.NoError(t, err, "the new channel's limit applies on its first admission")
	_, err = AcquireChannelConcurrency(ctx, recreated)
	require.ErrorIs(t, err, ErrConcurrencyFull, "the old request still occupies the reused ID")
	require.Equal(t, []ConcurrencySnapshot{{ID: 11, Limit: 2, InFlight: 2}}, gatewayConcurrency.snapshot("channel"))
	for range routeBreakerOpenFailures {
		recordRouteBreaker(staleEpoch, 11, "m", false)
	}
	routeObservations.coolDown(staleEpoch, 11, time.Now().Add(time.Minute).UnixMilli())
	health, weight := routeObservations.routeHealth(11, "m")
	require.Zero(t, health, "attempts sent before the delete cannot demote the new channel")
	require.Equal(t, 1.0, weight)
	held.Release()
	reused.Release()
	require.Equal(t, []ConcurrencySnapshot{{ID: 11, Limit: 2, InFlight: 0}}, gatewayConcurrency.snapshot("channel"))

	idle, err := AcquireChannelConcurrency(ctx, &model.Channel{Id: 22, MaxConcurrency: 1})
	require.NoError(t, err)
	ReleaseDeletedChannels(ctx, []int{22})
	idle.Release()
	for _, row := range gatewayConcurrency.snapshot("channel") {
		require.NotEqual(t, 22, row.ID, "a deleted channel leaves no entry once its last permit is released")
	}
}
