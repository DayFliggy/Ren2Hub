package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskTerminalRecoveryResumesAfterLogFailure(t *testing.T) {
	truncate(t)
	const id, previous, actual = 851, 30, 15
	seedUser(t, id, 100)
	seedToken(t, id, id, "task-terminal-recovery", 100)
	seedChannel(t, id)

	task := makeTask(id, id, previous, id, BillingSourceWallet)
	acceptTestTask(t, task)
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("completion_log_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			tx.AddError(assert.AnError)
		}
	}))
	err := completeTestTask(t, context.Background(), task, model.TaskStatusSuccess, actual, "actual usage")
	require.Error(t, err)
	require.NoError(t, model.DB.Callback().Create().Remove("completion_log_failure"))
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
	assert.EqualValues(t, actual, persisted.Quota)
	assert.Equal(t, 85, getUserQuota(t, id))
	assert.Zero(t, countLogs(t))
	requestID := task.PrivateData.SubmissionRequestID
	markBillingRecoveryStale(t, requestID)
	summary, err := RecoverStaleBillingRecoveries(context.Background(), common.GetTimestamp()-30, 100)
	require.NoError(t, err)
	assert.Equal(t, 1, summary.Settled)
	assert.Zero(t, summary.Failed)
	assert.EqualValues(t, 1, countLogs(t))
	assert.Equal(t, 85, getUserQuota(t, id))
	assert.Equal(t, 85, getTokenRemainQuota(t, id))
	used, requests := getUserUsageAccounting(t, id)
	assert.Equal(t, actual, used)
	assert.Equal(t, 1, requests)
	summary, err = RecoverStaleBillingRecoveries(context.Background(), common.GetTimestamp(), 100)
	require.NoError(t, err)
	assert.Zero(t, summary.Scanned)
}

type ambiguousTaskPollingAdaptor struct {
	taskPollingFetchAdaptor
	status int
}

func (a *ambiguousTaskPollingAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporarily unavailable"}}`))}, nil
}

func (a *ambiguousTaskPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{}, nil
}

func TestTaskPollingErrorsNeverBecomeJobFailure(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			truncate(t)
			seedTaskPollingChannel(t, 1, true)
			task := seedPollingTask(t, 1, "task_polling_error", "provider-job")
			var channel model.Channel
			require.NoError(t, model.DB.First(&channel, 1).Error)
			err := updateVideoSingleTask(context.Background(), &ambiguousTaskPollingAdaptor{status: status}, &channel,
				task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task})
			require.Error(t, err)
			var persisted model.Task
			require.NoError(t, model.DB.First(&persisted, task.ID).Error)
			assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
			var recoveries int64
			require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
			assert.EqualValues(t, 1, recoveries)
		})
	}
}

func TestTaskPollingOwnerGroupsDoNotBlockOtherChannels(t *testing.T) {
	truncate(t)
	seedTaskPollingChannel(t, 1, true)
	seedTaskPollingChannel(t, 2, true)
	slow := seedPollingTask(t, 1, "slow-owner", "slow-provider")
	fast := seedPollingTask(t, 2, "fast-owner", "fast-provider")
	adaptor := &taskPollingFetchAdaptor{fetched: make(chan string, 2), blockTaskID: slow.GetUpstreamTaskID(),
		blockStarted: make(chan struct{}), releaseBlock: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(adaptor.releaseBlock) }) })
	previous, limit := GetTaskAdaptorFunc, constant.TaskQueryLimit
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { GetTaskAdaptorFunc, constant.TaskQueryLimit = previous, limit })
	done := make(chan TaskPollSummary, 1)
	go func() { done <- RunTaskPollingOnce(context.Background(), nil) }()
	select {
	case <-adaptor.blockStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow provider did not start")
	}
	select {
	case id := <-adaptor.fetched:
		assert.Equal(t, fast.GetUpstreamTaskID(), id)
	case <-time.After(2 * time.Second):
		t.Fatal("an unrelated channel was blocked by another owner")
	}
	release.Do(func() { close(adaptor.releaseBlock) })
	select {
	case summary := <-done:
		assert.Equal(t, 2, summary.UnfinishedTasks)
	case <-time.After(2 * time.Second):
		t.Fatal("poll did not finish")
	}
}
