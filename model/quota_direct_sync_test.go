package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Stop exactly the mutation's first cache write, leaving hydration's Redis
// commands free to run. This works for both the old delta and snapshot writes.
type quotaSyncBarrier struct {
	key                    string
	entered, release, done chan struct{}
	claimed                atomic.Bool
}

func (h *quotaSyncBarrier) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "eval" && len(cmd.Args()) > 3 && cmd.Args()[3] == h.key && h.claimed.CompareAndSwap(false, true) {
		close(h.entered)
		<-h.release
		return context.WithValue(ctx, h, true), nil
	}
	return ctx, nil
}
func (h *quotaSyncBarrier) AfterProcess(ctx context.Context, _ redis.Cmder) error {
	if ctx.Value(h) == true {
		close(h.done)
	}
	return nil
}
func (*quotaSyncBarrier) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (*quotaSyncBarrier) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func TestQuotaDirectSyncHydrationDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openNativeTopUpFacadeDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&Token{}, &Checkin{}))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Token{}, &Checkin{})) })
			for _, mode := range []string{"user", "token", "user_force_db", "external_credit", "checkin", "admin"} {
				for _, delta := range []int{5, -5} {
					if (mode == "external_credit" || mode == "checkin") && delta < 0 {
						continue
					}
					t.Run(fmt.Sprintf("%s/delta%d", mode, delta), func(t *testing.T) {
						resetBatchUpdateTestState(t)
						useUserCacheMiniRedis(t)
						common.BatchUpdateEnabled = mode == "user_force_db"
						user := createReserveTestUser(t, 10)
						token := createReserveTestToken(t, 10)
						key := getUserCacheKey(user.Id)
						if mode == "token" {
							key = getTokenCacheKey(token.Key)
						}
						hook := &quotaSyncBarrier{key: key, entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
						var once sync.Once
						resume := func() { once.Do(func() { close(hook.release) }) }
						defer resume()
						common.RDB.AddHook(hook)
						changed := make(chan error, 1)
						go func() {
							var err error
							switch mode {
							case "checkin":
								_, err = userCheckinWithTransaction(&Checkin{UserId: user.Id, CheckinDate: "2026-11-22", QuotaAwarded: delta}, user.Id, delta)
							case "admin":
								if delta > 0 {
									_, err = AdjustUserQuota(user.Id, common.RoleRootUser, "add", delta)
								} else {
									_, err = AdjustUserQuota(user.Id, common.RoleRootUser, "subtract", -delta)
								}
							case "token":
								if delta > 0 {
									err = IncreaseTokenQuota(token.Id, token.Key, delta)
								} else {
									err = DecreaseTokenQuota(token.Id, token.Key, -delta)
								}
							case "external_credit":
								err = db.Transaction(func(tx *gorm.DB) error {
									return tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + ?", delta)).Error
								})
								if err == nil {
									syncCreditUserQuotaCache(user.Id, delta, "test committed credit")
								}
							default:
								if delta > 0 {
									err = IncreaseUserQuota(user.Id, delta, mode == "user_force_db")
								} else {
									err = DecreaseUserQuota(user.Id, -delta, mode == "user_force_db")
								}
							}
							changed <- err
						}()
						<-hook.entered
						changedEarly := false
						if mode != "external_credit" {
							select {
							case err := <-changed:
								require.NoError(t, err)
								changedEarly = true
							case <-time.After(40 * time.Millisecond):
							}
						}
						// The DB mutation has committed before the old cache delta can execute.
						// External credit waits synchronously on Redis even in the old code, so
						// detect serialization by observing hydration rather than its return.
						hydrated := make(chan int, 1)
						hydrateErr := make(chan error, 1)
						go func() {
							if mode == "token" {
								v, e := GetTokenByKey(token.Key, false)
								hydrateErr <- e
								if e == nil {
									hydrated <- v.RemainQuota
								}
							} else {
								v, e := GetUserCache(user.Id)
								hydrateErr <- e
								if e == nil {
									hydrated <- v.Quota
								}
							}
						}()
						hydratedEarly := false
						select {
						case err := <-hydrateErr:
							require.NoError(t, err)
							assert.Equal(t, 10+delta, <-hydrated)
							hydratedEarly = true
						case <-time.After(40 * time.Millisecond):
						}
						resume()
						<-hook.done
						if !changedEarly {
							require.NoError(t, <-changed)
						}
						if !hydratedEarly {
							require.NoError(t, <-hydrateErr)
							assert.Equal(t, 10+delta, <-hydrated)
						}
						assert.False(t, hydratedEarly, "hydration must share the mutation's cache boundary")
						var cached int
						var reserved bool
						var err error
						if mode == "token" {
							v, e := GetTokenByKey(token.Key, false)
							require.NoError(t, e)
							cached = v.RemainQuota
							reserved, err = TryReserveTokenQuota(token.Id, token.Key, 11+delta, false)
						} else {
							v, e := GetUserCache(user.Id)
							require.NoError(t, e)
							cached = v.Quota
							reserved, err = TryReserveUserQuota(user.Id, 11+delta)
						}
						assert.Equal(t, 10+delta, cached, "committed quota must not be applied twice")
						require.NoError(t, err)
						assert.False(t, reserved, "the cache must not permit spending beyond the committed balance")
						batchUpdate()
						if mode == "token" {
							saved := getTokenFromDB(t, token.Id)
							assert.Equal(t, 10+delta, saved.RemainQuota)
							assert.Equal(t, -delta, saved.UsedQuota)
						} else {
							assert.Equal(t, 10+delta, getUserQuotaFromDB(t, user.Id))
						}
					})
				}
			}
		})
	}
}

func TestCommittedCreditAfterHydrationPreservesPendingReservation(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprint(batch), func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = batch
			user := createReserveTestUser(t, 10)
			require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
				return tx.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + 5")).Error
			}))
			// Hydration sees the committed credit before its notifier runs. A new
			// request then consumes part of it, possibly only queued for persistence.
			v, err := GetUserCache(user.Id)
			require.NoError(t, err)
			require.Equal(t, 15, v.Quota)
			ok, err := TryReserveUserQuota(user.Id, 8)
			require.NoError(t, err)
			require.True(t, ok)
			syncCreditUserQuotaCache(user.Id, 5, "delayed committed credit")
			v, err = GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 7, v.Quota)
			// A repeated notifier re-reads current committed state, never credits twice.
			syncCreditUserQuotaCache(user.Id, 5, "repeated committed credit")
			v, err = GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 7, v.Quota)
			ok, err = TryReserveUserQuota(user.Id, 8)
			require.NoError(t, err)
			assert.False(t, ok)
			batchUpdate()
			assert.Equal(t, 7, getUserQuotaFromDB(t, user.Id))
		})
	}
}

func TestQuotaDirectSyncFailureRecovery(t *testing.T) {
	for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			server := useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 10)
			token := createReserveTestToken(t, 10)
			_, err := GetUserCache(user.Id)
			require.NoError(t, err)
			_, err = GetTokenByKey(token.Key, false)
			require.NoError(t, err)
			server.SetError("injected Redis failure")
			if kind == BatchUpdateTypeUserQuota {
				err = IncreaseUserQuota(user.Id, 5, true)
			} else {
				err = IncreaseTokenQuota(token.Id, token.Key, 5)
			}
			require.NoError(t, err, "a committed DB mutation must not invite retries because cache publication failed")
			id := user.Id
			if kind == BatchUpdateTypeTokenQuota {
				id = token.Id
			}
			assert.True(t, quotaCacheDirty(kind, id), "failed cache publication must force a future authoritative reload")
			server.SetError("")
			if kind == BatchUpdateTypeUserQuota {
				v, e := GetUserCache(user.Id)
				require.NoError(t, e)
				assert.Equal(t, 15, v.Quota)
				assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))
			} else {
				v, e := GetTokenByKey(token.Key, false)
				require.NoError(t, e)
				assert.Equal(t, 15, v.RemainQuota)
				assert.Equal(t, -5, v.UsedQuota)
				assert.Equal(t, 15, getTokenFromDB(t, token.Id).RemainQuota)
			}
		})
	}
}

func TestCommittedCreditFlushFailureKeepsPendingDebit(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	ok, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota + 5")).Error)
	injected := errors.New("injected pending debit failure")
	name := "committed_credit_flush_error"
	require.NoError(t, DB.Callback().Update().Before("gorm:begin_transaction").Register(name, func(tx *gorm.DB) { tx.AddError(injected) }))
	defer DB.Callback().Update().Remove(name)
	syncCreditUserQuotaCache(user.Id, 5, "committed test credit")
	assert.True(t, quotaCacheDirty(BatchUpdateTypeUserQuota, user.Id))
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	pending := batchUpdateStores[BatchUpdateTypeUserQuota][user.Id]
	batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	assert.Equal(t, -8, pending)
	assert.Equal(t, 15, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, DB.Callback().Update().Remove(name))
	v, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 7, v.Quota)
	batchUpdate()
	assert.Equal(t, 7, getUserQuotaFromDB(t, user.Id))
}

func TestQuotaDirectDBFailureDoesNotChangeCache(t *testing.T) {
	for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
		for _, delta := range []int{5, -5} {
			t.Run(fmt.Sprintf("kind%d/delta%d", kind, delta), func(t *testing.T) {
				truncateTables(t)
				resetBatchUpdateTestState(t)
				useUserCacheMiniRedis(t)
				user := createReserveTestUser(t, 10)
				token := createReserveTestToken(t, 10)
				_, err := GetUserCache(user.Id)
				require.NoError(t, err)
				_, err = GetTokenByKey(token.Key, false)
				require.NoError(t, err)
				injected := errors.New("injected direct DB failure")
				name := "direct_quota_update_error"
				require.NoError(t, DB.Callback().Update().Before("gorm:begin_transaction").Register(name, func(tx *gorm.DB) { tx.AddError(injected) }))
				defer DB.Callback().Update().Remove(name)
				if kind == BatchUpdateTypeUserQuota {
					if delta > 0 {
						err = IncreaseUserQuota(user.Id, delta, true)
					} else {
						err = DecreaseUserQuota(user.Id, -delta, true)
					}
				} else if delta > 0 {
					err = IncreaseTokenQuota(token.Id, token.Key, delta)
				} else {
					err = DecreaseTokenQuota(token.Id, token.Key, -delta)
				}
				require.ErrorIs(t, err, injected)
				// Let any legacy goroutine finish so an early successful snapshot cannot
				// hide a forbidden cache mutation after a failed database write.
				time.Sleep(30 * time.Millisecond)
				if kind == BatchUpdateTypeUserQuota {
					v, e := cacheGetUserBase(user.Id)
					require.NoError(t, e)
					assert.Equal(t, 10, v.Quota)
				} else {
					v, e := cacheGetTokenByKey(token.Key)
					require.NoError(t, e)
					assert.Equal(t, 10, v.RemainQuota)
					assert.Zero(t, v.UsedQuota)
				}
			})
		}
	}
}

func TestAdminDebitWaitsForQuotaSnapshotPublication(t *testing.T) {
	for _, mode := range []string{"subtract", "override"} {
		t.Run(mode, func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			useUserCacheMiniRedis(t)
			user := createReserveTestUser(t, 10)
			_, err := GetUserCache(user.Id)
			require.NoError(t, err)
			hook := &quotaSyncBarrier{key: getUserCacheKey(user.Id), entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
			var once sync.Once
			resume := func() { once.Do(func() { close(hook.release) }) }
			defer resume()
			common.RDB.AddHook(hook)
			snapshotDone := make(chan struct{})
			go func() { syncCreditUserQuotaCache(user.Id, 1, "delayed snapshot"); close(snapshotDone) }()
			<-hook.entered
			updateEntered := make(chan struct{})
			var observed sync.Once
			name := "admin_debit_snapshot_barrier"
			require.NoError(t, DB.Callback().Update().Before("gorm:begin_transaction").Register(name, func(*gorm.DB) { observed.Do(func() { close(updateEntered) }) }))
			defer DB.Callback().Update().Remove(name)
			adjusted := make(chan error, 1)
			go func() { _, err := AdjustUserQuota(user.Id, common.RoleRootUser, mode, 5); adjusted <- err }()
			crossed := false
			select {
			case <-updateEntered:
				crossed = true
			case <-time.After(40 * time.Millisecond):
			}
			resume()
			<-snapshotDone
			require.NoError(t, <-adjusted)
			assert.False(t, crossed, "a debit must not lower the DB balance while another publisher holds its earlier snapshot")
			v, err := GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 5, v.Quota)
			ok, err := TryReserveUserQuota(user.Id, 6)
			require.NoError(t, err)
			assert.False(t, ok)
		})
	}
}

func TestQuotaDirectSnapshotPreservesPendingAndCacheMetadata(t *testing.T) {
	for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
		for _, delta := range []int{5, -1} {
			t.Run(fmt.Sprintf("kind%d/delta%d", kind, delta), func(t *testing.T) {
				truncateTables(t)
				resetBatchUpdateTestState(t)
				useUserCacheMiniRedis(t)
				common.BatchUpdateEnabled = true
				user := createReserveTestUser(t, 10)
				token := createReserveTestToken(t, 10)
				key := getUserCacheKey(user.Id)
				var ok bool
				var err error
				if kind == BatchUpdateTypeUserQuota {
					ok, err = TryReserveUserQuota(user.Id, 8)
				} else {
					key = getTokenCacheKey(token.Key)
					ok, err = TryReserveTokenQuota(token.Id, token.Key, 8, false)
				}
				require.NoError(t, err)
				require.True(t, ok)
				before, err := common.RDB.HGetAll(t.Context(), key).Result()
				require.NoError(t, err)
				// Token writes have no db flag; a runtime switch to direct mode can still
				// leave earlier reservations queued, which the snapshot must preserve.
				if kind == BatchUpdateTypeTokenQuota {
					common.BatchUpdateEnabled = false
				}
				if kind == BatchUpdateTypeUserQuota {
					if delta > 0 {
						err = IncreaseUserQuota(user.Id, delta, true)
					} else {
						err = DecreaseUserQuota(user.Id, -delta, true)
					}
				} else if delta > 0 {
					err = IncreaseTokenQuota(token.Id, token.Key, delta)
				} else {
					err = DecreaseTokenQuota(token.Id, token.Key, -delta)
				}
				require.NoError(t, err)
				after, err := common.RDB.HGetAll(t.Context(), key).Result()
				require.NoError(t, err)
				for _, field := range []string{"Quota", "RemainQuota", "UsedQuota", "AccessedTime"} {
					delete(before, field)
					delete(after, field)
				}
				assert.Equal(t, before, after, "snapshot publication must preserve authentication metadata and its MAC")
				if kind == BatchUpdateTypeUserQuota {
					v, e := GetUserCache(user.Id)
					require.NoError(t, e)
					assert.Equal(t, 2+delta, v.Quota)
					assert.Equal(t, 2+delta, getUserQuotaFromDB(t, user.Id))
				} else {
					v, e := GetTokenByKey(token.Key, false)
					require.NoError(t, e)
					assert.Equal(t, 2+delta, v.RemainQuota)
					assert.Equal(t, 8-delta, v.UsedQuota)
					assert.Equal(t, 2+delta, getTokenFromDB(t, token.Id).RemainQuota)
				}
			})
		}
	}
}
