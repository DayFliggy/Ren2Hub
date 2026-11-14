//go:build known_upstream_restored

package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/bytedance/gopkg/util/gopool"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRestoredProbeUpdateVideoTasksRefundsPreConsumedWhenChannelMissing(t *testing.T) {
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

func TestRestoredProbeUpdateBatchTasksMissingChannelRefundsOnceAndKeepsLegacyRule(t *testing.T) {
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
func TestRestoredProbeUpdateBatchTasksKeepsTasksWhenChannelReadFails(t *testing.T) {
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
func TestRestoredProbeUpdateVideoTasksKeepsTasksWhenOnlyTheCacheMissesTheChannel(t *testing.T) {
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

// restoredFailingVideoPollingAdaptor records each fetch and answers it with a 404, so
// the poll fails and refunds the task it was given.
type restoredFailingVideoPollingAdaptor struct{ taskPollingFetchAdaptor }

func (a *restoredFailingVideoPollingAdaptor) FetchTask(ctx context.Context, baseURL, key string, task *model.Task, proxy string) (*http.Response, error) {
	resp, err := a.taskPollingFetchAdaptor.FetchTask(ctx, baseURL, key, task, proxy)
	if err != nil {
		return nil, err
	}
	_ = resp.Body.Close()
	return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
}

// Tasks on two channels share an upstream ID. One poll handles only the oldest
// task, with its own channel, whether that channel is deleted or answers with a
// failure; the other task keeps its pre-consumed quota for a later round.
func TestRestoredProbeRunTaskPollingOnceHandlesOneTaskPerUpstreamID(t *testing.T) {
	// The production UpdateVideoTasks already sorts channel IDs before enqueueing.
	// A single worker makes that existing order deterministic; production code
	// and the real RunTaskPollingOnce entry point remain unchanged. This fixture
	// isolates wrong identity attribution from the additional shared-pointer race.
	require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, time.Second, time.Millisecond)
	gopool.SetCap(1)
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, time.Second, time.Millisecond)
		gopool.SetCap(math.MaxInt32)
	})

	for _, firstChannelDeleted := range []bool{true, false} {
		t.Run(fmt.Sprintf("first channel deleted %v", firstChannelDeleted), func(t *testing.T) {
			truncate(t)
			const firstChannel, secondChannel, preConsumed = 27, 28, 1000
			for _, id := range []int{firstChannel, secondChannel} {
				require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("shared_upstream_user_%d", id), AffCode: fmt.Sprintf("shared_upstream_aff_%d", id), Quota: 5000, Status: common.UserStatusEnabled}).Error)
			}
			if !firstChannelDeleted {
				seedChannel(t, firstChannel)
			}
			seedChannel(t, secondChannel)
			tasks := map[int]*model.Task{}
			for _, id := range []int{firstChannel, secondChannel} {
				task := makeTask(id, id, preConsumed, 0, BillingSourceWallet)
				task.TaskID = fmt.Sprintf("task_shared_upstream_%d", id)
				task.Platform = constant.TaskPlatform("kling")
				task.SubmitTime = time.Now().Unix()
				task.PrivateData.UpstreamTaskID = "shared_upstream_id"
				require.NoError(t, model.DB.Create(task).Error)
				tasks[id] = task
			}

			adaptor := &restoredFailingVideoPollingAdaptor{}
			previousFactory, previousCache := GetTaskAdaptorFunc, common.MemoryCacheEnabled
			previousTimeout, previousLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
			GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
			common.MemoryCacheEnabled = false
			constant.TaskTimeoutMinutes = 0
			constant.TaskQueryLimit = 100
			t.Cleanup(func() {
				GetTaskAdaptorFunc, common.MemoryCacheEnabled = previousFactory, previousCache
				constant.TaskTimeoutMinutes, constant.TaskQueryLimit = previousTimeout, previousLimit
			})
			RunTaskPollingOnce(context.Background(), nil)
			require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, time.Second, time.Millisecond)

			var first, second model.Task
			require.NoError(t, model.DB.First(&first, tasks[firstChannel].ID).Error)
			require.NoError(t, model.DB.First(&second, tasks[secondChannel].ID).Error)
			assert.EqualValues(t, model.TaskStatusFailure, first.Status)
			assert.Zero(t, first.Quota)
			assert.Equal(t, 5000+preConsumed, getUserQuota(t, firstChannel))
			assert.EqualValues(t, model.TaskStatusInProgress, second.Status)
			assert.Equal(t, preConsumed, second.Quota)
			assert.Equal(t, 5000, getUserQuota(t, secondChannel))
			assert.Equal(t, int64(1), countLogs(t))
			wantFetches := 1
			if firstChannelDeleted {
				wantFetches = 0
			}
			assert.Equal(t, wantFetches, adaptor.fetchCount())
		})
	}
}
