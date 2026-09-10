package model

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func settlementFixture(t *testing.T) (User, Token, Task) {
	t.Helper()
	truncateTables(t)
	resetBatchUpdateTestState(t)
	user := createReserveTestUser(t, 700)
	user.UsedQuota = 300
	require.NoError(t, DB.Model(&user).Update("used_quota", 300).Error)
	token := Token{UserId: user.Id, Key: "settlement-fixture", RemainQuota: 700, UsedQuota: 300, Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)
	channel := Channel{Name: "settlement-fixture", UsedQuota: 300}
	require.NoError(t, DB.Create(&channel).Error)
	task := Task{TaskID: "upstream-test", UserId: user.Id, ChannelId: channel.Id, Quota: 300,
		PrivateData: TaskPrivateData{TokenId: token.Id, BillingSource: "wallet"}}
	require.NoError(t, DB.Create(&task).Error)
	return user, token, task
}

func TestTaskSettlementConcurrentRetryChangesFundsOnce(t *testing.T) {
	user, token, task := settlementFixture(t)
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func(copy Task) {
			defer wg.Done()
			<-start
			_, err := ApplyTaskSettlement(ctx, &copy, 0, true, RecordTaskBillingLogParams{ChannelId: copy.ChannelId})
			results <- err
		}(task)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
	assert.Equal(t, 1000, getTokenFromDB(t, token.Id).RemainQuota)
	var current Task
	require.NoError(t, DB.First(&current, task.ID).Error)
	assert.Zero(t, current.Quota)
	_, err := ApplyTaskSettlement(ctx, &task, 100, false, RecordTaskBillingLogParams{})
	assert.ErrorIs(t, err, ErrTaskSettlementConflict)
	assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
}

func TestTaskCompletionIntentSurvivesStopBeforeAccounting(t *testing.T) {
	user, token, task := settlementFixture(t)
	ctx := context.Background()
	from := task.Status
	task.Status = TaskStatusSuccess
	won, err := UpdateTaskWithSettlement(ctx, &task, from, TaskSettlementInput{FinalQuota: 150})
	require.NoError(t, err)
	assert.True(t, won)
	assert.Equal(t, 700, getUserQuotaFromDB(t, user.Id))
	var pending TaskSettlement
	require.NoError(t, DB.First(&pending, "task_id = ?", task.ID).Error)
	assert.False(t, pending.Applied)
	receipt, err := ResumeTaskSettlement(ctx, task.ID)
	require.NoError(t, err)
	assert.True(t, receipt.Applied)
	assert.Equal(t, 850, getUserQuotaFromDB(t, user.Id))
	assert.Equal(t, 850, getTokenFromDB(t, token.Id).RemainQuota)
	var current Task
	require.NoError(t, DB.First(&current, task.ID).Error)
	assert.EqualValues(t, TaskStatusSuccess, current.Status)
	assert.Equal(t, "settled", current.BillingState)
	assert.Equal(t, 150, current.Quota)
}

func TestTaskSettlementRollbackPreservesIntentAndAllAmounts(t *testing.T) {
	user, token, task := settlementFixture(t)
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register("fail-task-quota", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(errors.New("injected task write failure"))
		}
	}))
	t.Cleanup(func() { _ = DB.Callback().Update().Remove("fail-task-quota") })
	_, err := ApplyTaskSettlement(context.Background(), &task, 150, false, RecordTaskBillingLogParams{})
	require.Error(t, err)
	assert.Equal(t, 700, getUserQuotaFromDB(t, user.Id))
	assert.Equal(t, 700, getTokenFromDB(t, token.Id).RemainQuota)
	var pending TaskSettlement
	require.NoError(t, DB.First(&pending, "task_id = ?", task.ID).Error)
	assert.False(t, pending.Applied)
	require.NoError(t, DB.Callback().Update().Remove("fail-task-quota"))
	receipt, err := ResumeTaskSettlement(context.Background(), task.ID)
	require.NoError(t, err)
	assert.True(t, receipt.Applied)
	assert.Equal(t, 850, getUserQuotaFromDB(t, user.Id))
}

func TestTaskSettlementDeletedTokenDoesNotRestoreAccess(t *testing.T) {
	for _, hard := range []bool{false, true} {
		t.Run(map[bool]string{false: "soft", true: "physical"}[hard], func(t *testing.T) {
			user, token, task := settlementFixture(t)
			tx := DB
			if hard {
				tx = tx.Unscoped()
			}
			require.NoError(t, tx.Delete(&token).Error)
			receipt, err := ApplyTaskSettlement(context.Background(), &task, 0, true, RecordTaskBillingLogParams{})
			require.NoError(t, err)
			assert.Equal(t, hard, receipt.TokenMissing)
			assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
			var reloaded Token
			assert.ErrorIs(t, DB.First(&reloaded, token.Id).Error, gorm.ErrRecordNotFound)
			if !hard {
				require.NoError(t, DB.Unscoped().First(&reloaded, token.Id).Error)
				assert.Equal(t, 1000, reloaded.RemainQuota)
			}
		})
	}
}

func TestTaskSettlementCachePreservesOtherInstancePendingQuota(t *testing.T) {
	user, token, task := settlementFixture(t)
	useUserCacheMiniRedis(t)
	ctx := context.Background()
	require.NoError(t, populateUserCache(user))
	_, err := cacheInitToken(token)
	require.NoError(t, err)
	// These deltas deliberately have no local batch record: another process owns them.
	require.NoError(t, common.RDB.HIncrBy(ctx, getUserCacheKey(user.Id), "Quota", -200).Err())
	require.NoError(t, common.RDB.HIncrBy(ctx, getTokenCacheKey(token.Key), "RemainQuota", -200).Err())
	require.NoError(t, common.RDB.HIncrBy(ctx, getTokenCacheKey(token.Key), "UsedQuota", 200).Err())
	_, err = ApplyTaskSettlement(ctx, &task, 150, false, RecordTaskBillingLogParams{})
	require.NoError(t, err)
	result, err := cacheTryReserveUserQuota(user.Id, 1)
	require.NoError(t, err)
	assert.Equal(t, cacheQuotaSettlementPending, result)
	require.NoError(t, SyncTaskSettlementCache(ctx, task.ID))
	require.NoError(t, SyncTaskSettlementCache(ctx, task.ID))
	assert.Equal(t, "650", common.RDB.HGet(ctx, getUserCacheKey(user.Id), "Quota").Val())
	assert.Equal(t, "650", common.RDB.HGet(ctx, getTokenCacheKey(token.Key), "RemainQuota").Val())
	assert.Equal(t, "350", common.RDB.HGet(ctx, getTokenCacheKey(token.Key), "UsedQuota").Val())
	assert.Equal(t, 850, getUserQuotaFromDB(t, user.Id))
	// A delayed metadata refresh preserves the newer live quota.
	require.NoError(t, updateUserCache(user))
	assert.Equal(t, "650", common.RDB.HGet(ctx, getUserCacheKey(user.Id), "Quota").Val())
	// Once the hash expires, an old DB snapshot must not resurrect its balance.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("quota", gorm.Expr("quota - ?", 200)).Error)
	require.NoError(t, common.RDB.Del(ctx, getUserCacheKey(user.Id)).Err())
	assert.ErrorIs(t, populateUserCache(user), ErrTaskQuotaCachePending)
	reserved, err := TryReserveUserQuota(user.Id, 600)
	require.NoError(t, err)
	assert.True(t, reserved)
}

func TestTaskSettlementLogFailureDoesNotRepeatFunds(t *testing.T) {
	user, _, task := settlementFixture(t)
	ctx := context.Background()
	_, err := ApplyTaskSettlement(ctx, &task, 0, true, RecordTaskBillingLogParams{ChannelId: task.ChannelId, ModelName: "test-model"})
	require.NoError(t, err)
	original := LOG_DB
	logDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	LOG_DB = logDB
	t.Cleanup(func() { LOG_DB = original; sql, _ := logDB.DB(); _ = sql.Close() })
	require.Error(t, DeliverTaskSettlementLog(ctx, task.ID))
	assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
	require.NoError(t, logDB.AutoMigrate(&Log{}, &TaskSettlementLogReceipt{}))
	require.NoError(t, DeliverTaskSettlementLog(ctx, task.ID))
	require.NoError(t, DeliverTaskSettlementLog(ctx, task.ID))
	var count int64
	require.NoError(t, logDB.Model(&Log{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	assert.Equal(t, 1000, getUserQuotaFromDB(t, user.Id))
}
