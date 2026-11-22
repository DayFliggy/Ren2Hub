package model

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type quotaDeltaBarrier struct {
	script                 string
	entered, release, done chan struct{}
	before, after          sync.Once
}

func (h *quotaDeltaBarrier) matches(cmd redis.Cmder) bool {
	return cmd.Name() == "eval" && len(cmd.Args()) > 1 && cmd.Args()[1] == h.script
}
func (h *quotaDeltaBarrier) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if h.matches(cmd) {
		h.before.Do(func() { close(h.entered); <-h.release })
	}
	return ctx, nil
}
func (h *quotaDeltaBarrier) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	if h.matches(cmd) {
		h.after.Do(func() { close(h.done) })
	}
	return nil
}
func (*quotaDeltaBarrier) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (*quotaDeltaBarrier) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func TestQuotaBatchHydrationAtomicityDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openNativeTopUpFacadeDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&Token{}))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Token{})) })
			for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
				for _, delta := range []int{5, -5} {
					t.Run(fmt.Sprintf("kind%d/delta%d", kind, delta), func(t *testing.T) {
						resetBatchUpdateTestState(t)
						useUserCacheMiniRedis(t)
						common.BatchUpdateEnabled = true
						user := createReserveTestUser(t, 10)
						token := createReserveTestToken(t, 10)
						script := userQuotaDeltaScript
						if kind == BatchUpdateTypeTokenQuota {
							script = tokenQuotaDeltaScript
						}
						hook := &quotaDeltaBarrier{script: script, entered: make(chan struct{}), release: make(chan struct{}), done: make(chan struct{})}
						var once sync.Once
						resume := func() { once.Do(func() { close(hook.release) }) }
						defer resume()
						common.RDB.AddHook(hook)
						changed := make(chan error, 1)
						go func() {
							if kind == BatchUpdateTypeUserQuota {
								changed <- DeltaUpdateUserQuota(user.Id, delta)
							} else if delta > 0 {
								changed <- IncreaseTokenQuota(token.Id, token.Key, delta)
							} else {
								changed <- DecreaseTokenQuota(token.Id, token.Key, -delta)
							}
						}()
						<-hook.entered
						// A delayed Redis increment used to outlive the completed mutation. Force
						// hydration into that window when the old implementation returns early.
						returned := false
						select {
						case err := <-changed:
							require.NoError(t, err)
							returned = true
						case <-time.After(40 * time.Millisecond):
						}
						type snapshot struct {
							quota int
							err   error
						}
						hydrated := make(chan snapshot, 1)
						go func() {
							if kind == BatchUpdateTypeUserQuota {
								v, e := GetUserCache(user.Id)
								if e != nil {
									hydrated <- snapshot{err: e}
								} else {
									hydrated <- snapshot{quota: v.Quota}
								}
							} else {
								v, e := GetTokenByKey(token.Key, false)
								if e != nil {
									hydrated <- snapshot{err: e}
								} else {
									hydrated <- snapshot{quota: v.RemainQuota}
								}
							}
						}()
						var value snapshot
						if returned {
							value = <-hydrated
						}
						resume()
						<-hook.done
						if !returned {
							require.NoError(t, <-changed)
							value = <-hydrated
						}
						require.NoError(t, value.err)
						assert.Equal(t, 10+delta, value.quota)
						assert.False(t, returned, "a batch mutation must finish its cache delta before returning")
						var got int
						var ok bool
						var err error
						if kind == BatchUpdateTypeUserQuota {
							v, e := GetUserCache(user.Id)
							require.NoError(t, e)
							got = v.Quota
							ok, err = TryReserveUserQuota(user.Id, 11+delta)
						} else {
							v, e := GetTokenByKey(token.Key, false)
							require.NoError(t, e)
							got = v.RemainQuota
							ok, err = TryReserveTokenQuota(token.Id, token.Key, 11+delta, false)
						}
						assert.Equal(t, 10+delta, got, "hydration and delayed mutation must not count the same delta twice")
						require.NoError(t, err)
						assert.False(t, ok, "cannot spend more than the real balance")
						batchUpdate()
						if kind == BatchUpdateTypeUserQuota {
							assert.Equal(t, 10+delta, getUserQuotaFromDB(t, user.Id))
						} else {
							v := getTokenFromDB(t, token.Id)
							assert.Equal(t, 10+delta, v.RemainQuota)
							assert.Equal(t, -delta, v.UsedQuota)
						}
					})
				}
			}
		})
	}
}

func TestQuotaCacheHitsDoNotWaitForReservationShard(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	user := createReserveTestUser(t, 10)
	require.NoError(t, populateUserCache(user))
	token := createReserveTestToken(t, 10)
	_, err := cacheInitToken(token)
	require.NoError(t, err)
	for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			id := user.Id
			if kind == BatchUpdateTypeTokenQuota {
				id = token.Id
			}
			unlock := lockQuotaReservation(kind, id+256)
			done := make(chan error, 1)
			go func() {
				if kind == BatchUpdateTypeUserQuota {
					_, err := GetUserCache(user.Id)
					done <- err
				} else {
					_, err := GetTokenByKey(token.Key, false)
					done <- err
				}
			}()
			select {
			case err := <-done:
				unlock()
				require.NoError(t, err)
			case <-time.After(200 * time.Millisecond):
				unlock()
				require.NoError(t, <-done)
				t.Error("cache hit waited on another object's reservation shard")
			}
		})
	}
}

func TestQuotaBatchFlushReleasesGlobalLockAndMergesFailure(t *testing.T) {
	for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			truncateTables(t)
			resetBatchUpdateTestState(t)
			user := createReserveTestUser(t, 10)
			token := createReserveTestToken(t, 10)
			id := user.Id
			if kind == BatchUpdateTypeTokenQuota {
				id = token.Id
			}
			addNewRecord(kind, id, -3)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(release) }) }
			defer resume()
			injected := errors.New("injected flush failure")
			name := "quota_batch_failure_barrier"
			require.NoError(t, DB.Callback().Update().Before("gorm:begin_transaction").Register(name, func(tx *gorm.DB) { close(entered); <-release; tx.AddError(injected) }))
			defer DB.Callback().Update().Remove(name)
			flushed := make(chan error, 1)
			go func() {
				unlock := lockQuotaReservation(kind, id)
				defer unlock()
				flushed <- flushPendingQuota(kind, id)
			}()
			<-entered
			queued := make(chan struct{})
			go func() { addNewRecord(kind, id, 2); addNewRecord(kind, id+1, 4); close(queued) }()
			select {
			case <-queued:
			case <-time.After(200 * time.Millisecond):
				t.Error("database write held the global batch lock")
			}
			resume()
			require.ErrorIs(t, <-flushed, injected)
			<-queued
			batchUpdateLocks[kind].Lock()
			same, other := batchUpdateStores[kind][id], batchUpdateStores[kind][id+1]
			batchUpdateLocks[kind].Unlock()
			assert.Equal(t, -1, same, "failed detached delta must merge with newly queued credit")
			assert.Equal(t, 4, other)
			require.NoError(t, DB.Callback().Update().Remove(name))
			unlock := lockQuotaReservation(kind, id)
			err := flushPendingQuota(kind, id)
			unlock()
			require.NoError(t, err)
			if kind == BatchUpdateTypeUserQuota {
				assert.Equal(t, 9, getUserQuotaFromDB(t, id))
			} else {
				v := getTokenFromDB(t, id)
				assert.Equal(t, 9, v.RemainQuota)
				assert.Equal(t, 1, v.UsedQuota)
			}
		})
	}
}

func TestQuotaBatchDeletedBalancesDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openNativeTopUpFacadeDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&Token{}))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Token{})) })
			for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
				for _, hard := range []bool{false, true} {
					t.Run(fmt.Sprintf("kind%d/hard%t", kind, hard), func(t *testing.T) {
						resetBatchUpdateTestState(t)
						user := createReserveTestUser(t, 10)
						token := createReserveTestToken(t, 10)
						id := user.Id
						var record any = &user
						if kind == BatchUpdateTypeTokenQuota {
							id = token.Id
							record = &token
						}
						addNewRecord(kind, id, -3)
						deletion := db
						if hard {
							deletion = deletion.Unscoped()
						}
						require.NoError(t, deletion.Delete(record).Error)
						var logs bytes.Buffer
						common.LogWriterMu.Lock()
						old := gin.DefaultErrorWriter
						gin.DefaultErrorWriter = &logs
						common.LogWriterMu.Unlock()
						defer func() { common.LogWriterMu.Lock(); gin.DefaultErrorWriter = old; common.LogWriterMu.Unlock() }()
						batchUpdate()
						batchUpdate()
						batchUpdateLocks[kind].Lock()
						_, pending := batchUpdateStores[kind][id]
						batchUpdateLocks[kind].Unlock()
						assert.False(t, pending, "deleted balance must not retry forever")
						if hard {
							assert.Equal(t, 1, strings.Count(logs.String(), "[SYS]"))
							assert.Contains(t, logs.String(), fmt.Sprintf("id=%d", id))
							assert.Contains(t, logs.String(), "delta=-3")
						} else {
							assert.Empty(t, logs.String())
							if kind == BatchUpdateTypeUserQuota {
								var v User
								require.NoError(t, db.Unscoped().First(&v, id).Error)
								assert.Equal(t, 7, v.Quota)
								assert.True(t, v.DeletedAt.Valid)
							} else {
								var v Token
								require.NoError(t, db.Unscoped().First(&v, id).Error)
								assert.Equal(t, 7, v.RemainQuota)
								assert.Equal(t, 3, v.UsedQuota)
								assert.True(t, v.DeletedAt.Valid)
							}
						}
					})
				}
			}
		})
	}
}

func TestTokenQuotaHydrationRechecksCacheAfterLock(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	token := createReserveTestToken(t, 10)
	unlock := lockQuotaReservation(BatchUpdateTypeTokenQuota, token.Id)
	queried := make(chan struct{})
	var once sync.Once
	name := "quota_cache_second_lookup"
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register(name, func(*gorm.DB) { once.Do(func() { close(queried) }) }))
	defer DB.Callback().Query().Remove(name)
	type result struct {
		token *Token
		err   error
	}
	done := make(chan result, 1)
	go func() { v, e := GetTokenByKey(token.Key, false); done <- result{v, e} }()
	<-queried
	// Another lock owner has already hydrated this token while the reader waits.
	snapshot := token
	snapshot.RemainQuota = 7
	snapshot.UsedQuota = 3
	_, err := cacheInitToken(snapshot)
	unlock()
	require.NoError(t, err)
	got := <-done
	require.NoError(t, got.err)
	require.Equal(t, 7, got.token.RemainQuota)
	require.Equal(t, 3, got.token.UsedQuota)
}

func TestUnlimitedTokenBatchReservationDoesNotRelock(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	token := createReserveTestToken(t, 1)
	ok, err := TryReserveTokenQuota(token.Id, token.Key, 3, true)
	require.NoError(t, err)
	require.True(t, ok)
	batchUpdate()
	saved := getTokenFromDB(t, token.Id)
	require.Equal(t, -2, saved.RemainQuota)
	require.Equal(t, 3, saved.UsedQuota)
}

func TestQuotaAuthenticationReloadsDirtyCacheAfterRedisRecovery(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	token := createReserveTestToken(t, 10)
	_, err := GetUserCache(user.Id)
	require.NoError(t, err)
	_, err = GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	server.SetError("injected Redis outage")
	ok, err := TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = TryReserveTokenQuota(token.Id, token.Key, 3, false)
	require.NoError(t, err)
	require.True(t, ok)
	server.SetError("")
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, 7, cachedUser.Quota)
	cachedToken, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, 7, cachedToken.RemainQuota)
	assert.Equal(t, 3, cachedToken.UsedQuota)
	ok, err = TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = TryReserveTokenQuota(token.Id, token.Key, 8, false)
	require.NoError(t, err)
	require.False(t, ok)
}
