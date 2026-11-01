package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateVideoTasksRefundsPreConsumedWhenChannelMissing(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, tokenID, channelID = 21, 21, 21
	const initQuota, preConsumed, tokenRemain = 10000, 5000, 5000

	seedUser(t, userID, initQuota-preConsumed)
	seedToken(t, tokenID, userID, "sk-missing-channel", tokenRemain-preConsumed)
	seedChargedAccounting(t, userID, channelID, tokenID, preConsumed, 1)

	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet)
	task.PrivateData.UpstreamTaskID = "upstream_missing_channel"
	task.Status = model.TaskStatusInProgress
	require.NoError(t, model.DB.Create(task).Error)
	taskM := map[string]*model.Task{task.GetUpstreamTaskID(): task}

	err := updateVideoTasks(ctx, constant.TaskPlatform("kling"), channelID, []string{task.GetUpstreamTaskID()}, taskM)
	require.Error(t, err)

	var stored model.Task
	require.NoError(t, model.DB.Where("id = ?", task.ID).First(&stored).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), stored.Status)
	assert.Equal(t, "100%", stored.Progress)
	assert.Equal(t, 0, stored.Quota, "a refunded task no longer holds pre-consumed quota")
	assert.NotZero(t, stored.FinishTime)
	assert.Equal(t, initQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
}

func TestUpdateBatchTasksMissingChannelRefundsOnceAndKeepsLegacyRule(t *testing.T) {
	truncate(t)
	ctx := context.Background()

	const userID, channelID = 22, 22
	const initQuota, modernQuota, legacyQuota = 10_000, 1_200, 1_800
	seedUser(t, userID, initQuota)

	modern := makeTask(userID, channelID, modernQuota, 0, BillingSourceWallet)
	modern.TaskID = "batch_missing_channel_modern"
	modern.PrivateData.UpstreamTaskID = "upstream_batch_modern"
	modern.SubmitTime = model.TaskRefundLegacyCutoff
	require.NoError(t, model.DB.Create(modern).Error)

	legacy := makeTask(userID, channelID, legacyQuota, 0, BillingSourceWallet)
	legacy.TaskID = "batch_missing_channel_legacy"
	legacy.PrivateData.UpstreamTaskID = "upstream_batch_legacy"
	legacy.SubmitTime = model.TaskRefundLegacyCutoff - 1
	require.NoError(t, model.DB.Create(legacy).Error)

	ids := []string{modern.GetUpstreamTaskID(), legacy.GetUpstreamTaskID()}
	firstPoll := map[string]*model.Task{ids[0]: modern, ids[1]: legacy}
	require.NoError(t, updateBatchTasks(ctx, nil, channelID, ids, firstPoll))

	// A stale copy from a concurrent poll loses the CAS and must not refund again.
	var staleModern model.Task
	require.NoError(t, model.DB.First(&staleModern, modern.ID).Error)
	staleModern.Status = model.TaskStatusInProgress
	staleModern.Quota = modernQuota
	require.NoError(t, updateBatchTasks(ctx, nil, channelID, ids[:1], map[string]*model.Task{ids[0]: &staleModern}))

	var storedModern, storedLegacy model.Task
	require.NoError(t, model.DB.First(&storedModern, modern.ID).Error)
	require.NoError(t, model.DB.First(&storedLegacy, legacy.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, storedModern.Status)
	assert.EqualValues(t, model.TaskStatusFailure, storedLegacy.Status)
	assert.NotZero(t, storedModern.FinishTime)
	assert.NotZero(t, storedLegacy.FinishTime)
	assert.Zero(t, storedModern.Quota)
	assert.Zero(t, storedLegacy.Quota)
	assert.Contains(t, storedLegacy.FailReason, "old system")
	assert.Equal(t, initQuota+modernQuota, getUserQuota(t, userID), "only the modern task is refunded, once")
	assert.Equal(t, int64(1), countLogs(t))
}

// A failed channel read is not a deletion: the task keeps its pre-consumed
// quota and the error is returned so the next poll retries.
func TestUpdateBatchTasksKeepsTasksWhenChannelReadFails(t *testing.T) {
	truncate(t)
	const userID, channelID, preConsumed = 23, 23, 1000
	seedUser(t, userID, 5000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet)
	task.PrivateData.UpstreamTaskID = "upstream_channel_read_failure"
	require.NoError(t, model.DB.Create(task).Error)

	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	const callback = "test_channel_read_failure"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callback, func(db *gorm.DB) {
		if db.Statement.Table == "channels" {
			db.AddError(errors.New("injected channel read failure"))
		}
	}))
	ids := []string{task.GetUpstreamTaskID()}
	err := updateBatchTasks(context.Background(), nil, channelID, ids, map[string]*model.Task{ids[0]: task})
	require.NoError(t, model.DB.Callback().Query().Remove(callback))
	require.ErrorContains(t, err, "injected channel read failure")

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.Equal(t, preConsumed, stored.Quota)
	assert.Equal(t, 5000, getUserQuota(t, userID))
}

// The in-memory channel cache can miss a channel the database still has; only
// a database not-found ends the tasks.
func TestUpdateVideoTasksKeepsTasksWhenOnlyTheCacheMissesTheChannel(t *testing.T) {
	truncate(t)
	const userID, channelID, preConsumed = 24, 24, 1000
	seedUser(t, userID, 5000)
	seedChannel(t, channelID)
	task := makeTask(userID, channelID, preConsumed, 0, BillingSourceWallet)
	task.PrivateData.UpstreamTaskID = "upstream_cache_miss"
	require.NoError(t, model.DB.Create(task).Error)

	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	_, cacheErr := model.CacheGetChannel(channelID)
	require.Error(t, cacheErr, "the channel was created after the cache was built")

	ids := []string{task.GetUpstreamTaskID()}
	err := updateVideoTasks(context.Background(), constant.TaskPlatform("kling"), channelID, ids, map[string]*model.Task{ids[0]: task})
	require.ErrorContains(t, err, "still exists")

	var stored model.Task
	require.NoError(t, model.DB.First(&stored, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.Equal(t, preConsumed, stored.Quota)
	assert.Equal(t, 5000, getUserQuota(t, userID))
}

// taskM is keyed by upstream task ID across a platform's channels, so a live
// channel's task can occupy the slot of a deleted channel's task with the same
// upstream ID. Ending the deleted channel must not fail or refund the live one.
func TestFailTasksWithoutChannelSkipsAnotherChannelsTask(t *testing.T) {
	truncate(t)
	const missingChannel, liveChannel, preConsumed = 25, 26, 1000
	for _, id := range []int{missingChannel, liveChannel} {
		require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("missing_channel_user_%d", id), AffCode: fmt.Sprintf("missing_channel_aff_%d", id), Quota: 5000, Status: common.UserStatusEnabled}).Error)
	}
	seedChannel(t, liveChannel)
	tasks := map[int]*model.Task{}
	for _, id := range []int{missingChannel, liveChannel} {
		task := makeTask(id, id, preConsumed, 0, BillingSourceWallet)
		task.TaskID = fmt.Sprintf("task_missing_channel_%d", id)
		task.PrivateData.UpstreamTaskID = "shared_upstream_id"
		require.NoError(t, model.DB.Create(task).Error)
		tasks[id] = task
	}

	taskM := map[string]*model.Task{"shared_upstream_id": tasks[liveChannel]}
	require.NoError(t, updateBatchTasks(context.Background(), nil, missingChannel, []string{"shared_upstream_id"}, taskM))

	for _, id := range []int{missingChannel, liveChannel} {
		var stored model.Task
		require.NoError(t, model.DB.First(&stored, tasks[id].ID).Error)
		assert.EqualValues(t, model.TaskStatusInProgress, stored.Status, "channel %d", id)
		assert.Equal(t, preConsumed, stored.Quota, "channel %d", id)
		assert.Equal(t, 5000, getUserQuota(t, id), "channel %d", id)
	}
}
