package model

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestQuotaReserveRedisLossDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openNativeTopUpFacadeDB(t, dialect)
			require.NoError(t, db.AutoMigrate(&Token{}))
			t.Cleanup(func() { require.NoError(t, db.Migrator().DropTable(&Token{})) })
			resetBatchUpdateTestState(t)
			redis := useUserCacheMiniRedis(t)
			common.BatchUpdateEnabled = true
			user := createReserveTestUser(t, 10)
			token := createReserveTestToken(t, 10)
			ok, err := TryReserveUserQuota(user.Id, 8)
			require.NoError(t, err)
			require.True(t, ok)
			ok, err = TryReserveTokenQuota(token.Id, token.Key, 8, false)
			require.NoError(t, err)
			require.True(t, ok)
			redis.SetError("injected Redis failure")
			ok, err = TryReserveUserQuota(user.Id, 8)
			require.NoError(t, err)
			require.False(t, ok)
			ok, err = TryReserveTokenQuota(token.Id, token.Key, 8, false)
			require.NoError(t, err)
			require.False(t, ok)
			// A smaller reservation can still use the real remaining balance.
			ok, err = TryReserveUserQuota(user.Id, 1)
			require.NoError(t, err)
			require.True(t, ok)
			ok, err = TryReserveTokenQuota(token.Id, token.Key, 1, false)
			require.NoError(t, err)
			require.True(t, ok)
			redis.SetError("")
			ok, err = TryReserveUserQuota(user.Id, 2)
			require.NoError(t, err)
			require.False(t, ok)
			ok, err = TryReserveTokenQuota(token.Id, token.Key, 2, false)
			require.NoError(t, err)
			require.False(t, ok)
			batchUpdate()
			require.Equal(t, 1, getUserQuotaFromDB(t, user.Id))
			saved := getTokenFromDB(t, token.Id)
			require.Equal(t, 1, saved.RemainQuota)
			require.Equal(t, 9, saved.UsedQuota)
		})
	}
}

func TestQuotaReservePendingFlushFailureRetainsDebit(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	redis := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	ok, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.True(t, ok)
	redis.SetError("injected Redis failure")
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test_pending_flush_error", func(tx *gorm.DB) { tx.AddError(errors.New("injected DB failure")) }))
	t.Cleanup(func() { DB.Callback().Update().Remove("test_pending_flush_error") })
	ok, err = TryReserveUserQuota(user.Id, 1)
	require.Error(t, err)
	require.False(t, ok)
	require.NoError(t, DB.Callback().Update().Remove("test_pending_flush_error"))
	ok, err = TryReserveUserQuota(user.Id, 3)
	require.NoError(t, err)
	require.False(t, ok)
	batchUpdate()
	require.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
}

func TestQuotaReserveWaitsForBatchFlush(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	redis := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	ok, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.True(t, ok)
	redis.SetError("injected Redis failure")
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	resume := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(resume)
	var once sync.Once
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("test_flush_barrier", func(tx *gorm.DB) { once.Do(func() { close(entered); <-release }) }))
	t.Cleanup(func() { DB.Callback().Update().Remove("test_flush_barrier") })
	flushed := make(chan struct{})
	go func() { batchUpdate(); close(flushed) }()
	<-entered
	reserved := make(chan bool, 1)
	failures := make(chan error, 1)
	go func() { ok, err := TryReserveUserQuota(user.Id, 8); reserved <- ok; failures <- err }()
	select {
	case <-reserved:
		t.Fatal("reservation bypassed in-flight batch")
	case <-time.After(20 * time.Millisecond):
	}
	resume()
	<-flushed
	require.False(t, <-reserved)
	require.NoError(t, <-failures)
	require.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
}

func TestQuotaReserveRejectsStaleAuthenticationHydration(t *testing.T) {
	truncateTables(t)
	resetBatchUpdateTestState(t)
	redis := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true
	user := createReserveTestUser(t, 10)
	token := createReserveTestToken(t, 10)
	ok, err := TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = TryReserveTokenQuota(token.Id, token.Key, 8, false)
	require.NoError(t, err)
	require.True(t, ok)
	redis.Del(getUserCacheKey(user.Id))
	redis.Del(getTokenCacheKey(token.Key))
	// Authentication may hydrate both records before reservation is called.
	cachedUser, err := GetUserCache(user.Id)
	require.NoError(t, err)
	cachedToken, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	require.Equal(t, 2, cachedUser.Quota)
	require.Equal(t, 2, cachedToken.RemainQuota)
	ok, err = TryReserveUserQuota(user.Id, 8)
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = TryReserveTokenQuota(token.Id, token.Key, 8, false)
	require.NoError(t, err)
	require.False(t, ok)
	batchUpdate()
	require.Equal(t, 2, getUserQuotaFromDB(t, user.Id))
	require.Equal(t, 2, getTokenFromDB(t, token.Id).RemainQuota)
}
