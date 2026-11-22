package service

import (
	"context"
	"fmt"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

type channelIdentityPollingAdaptor struct {
	taskPollingFetchAdaptor
	seen chan [2]int
}

func (a *channelIdentityPollingAdaptor) FetchTask(ctx context.Context, baseURL, key string, task *model.Task, proxy string) (*http.Response, error) {
	a.seen <- [2]int{a.initChannelMeta().ChannelId, task.ChannelId}
	return a.taskPollingFetchAdaptor.FetchTask(ctx, baseURL, key, task, proxy)
}

func TestTaskPollingKeepsSameUpstreamIDOnSeparateChannels(t *testing.T) {
	truncate(t)
	oldFactory, oldCache := GetTaskAdaptorFunc, common.MemoryCacheEnabled
	oldTimeout, oldLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldFactory
		common.MemoryCacheEnabled = oldCache
		constant.TaskTimeoutMinutes = oldTimeout
		constant.TaskQueryLimit = oldLimit
	})
	common.MemoryCacheEnabled = false
	constant.TaskTimeoutMinutes = 0
	constant.TaskQueryLimit = 100
	seen := make(chan [2]int, 4)
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return &channelIdentityPollingAdaptor{seen: seen} }
	for _, id := range []int{27, 28} {
		seedChannel(t, id)
		task := makeTask(id, id, 1000, 0, BillingSourceWallet)
		task.TaskID = fmt.Sprintf("task_identity_%d", id)
		task.Platform = constant.TaskPlatform("kling")
		task.SubmitTime = time.Now().Unix()
		task.PrivateData.UpstreamTaskID = "shared_upstream_id"
		require.NoError(t, model.DB.Create(task).Error)
	}
	for i := 0; i < 2; i++ {
		result := RunTaskPollingOnce(context.Background(), nil)
		require.Equal(t, 2, result.UnfinishedTasks)
		for j := 0; j < 2; j++ {
			pair := <-seen
			require.Equal(t, pair[0], pair[1], "渠道响应只能更新本渠道任务")
		}
	}
	for _, id := range []int{27, 28} {
		var task model.Task
		require.NoError(t, model.DB.Where("task_id = ?", fmt.Sprintf("task_identity_%d", id)).First(&task).Error)
		require.Equal(t, "30%", task.Progress)
		require.Equal(t, 1000, task.Quota)
	}
}

type channelIdentityBatchAdaptor struct {
	batchPollingAdaptor
	seen chan [2]int
}

func (a *channelIdentityBatchAdaptor) FetchBatchTasks(ctx context.Context, baseURL, key string, tasks []*model.Task, proxy string) (*http.Response, error) {
	for _, task := range tasks {
		a.seen <- [2]int{a.initChannelMeta().ChannelId, task.ChannelId}
	}
	return a.batchPollingAdaptor.FetchBatchTasks(ctx, baseURL, key, tasks, proxy)
}

func TestTaskPollingBatchKeepsSameUpstreamIDOnSeparateChannels(t *testing.T) {
	truncate(t)
	oldFactory, oldCache := GetTaskAdaptorFunc, common.MemoryCacheEnabled
	oldTimeout, oldLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldFactory
		common.MemoryCacheEnabled = oldCache
		constant.TaskTimeoutMinutes = oldTimeout
		constant.TaskQueryLimit = oldLimit
	})
	common.MemoryCacheEnabled = false
	constant.TaskTimeoutMinutes = 0
	constant.TaskQueryLimit = 100
	seen := make(chan [2]int, 2)
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return &channelIdentityBatchAdaptor{seen: seen} }
	for _, id := range []int{37, 38} {
		seedChannel(t, id)
		task := makeTask(id, id, 1000, 0, BillingSourceWallet)
		task.TaskID = fmt.Sprintf("batch_identity_%d", id)
		task.Platform = constant.TaskPlatform("kling")
		task.SubmitTime = time.Now().Unix()
		task.PrivateData.UpstreamTaskID = "shared_batch_id"
		require.NoError(t, model.DB.Create(task).Error)
	}
	require.Equal(t, 2, RunTaskPollingOnce(context.Background(), nil).UnfinishedTasks)
	for i := 0; i < 2; i++ {
		pair := <-seen
		require.Equal(t, pair[0], pair[1])
	}
}

type terminalIdentityAdaptor struct{ taskPollingFetchAdaptor }

func (a *terminalIdentityAdaptor) FetchTask(context.Context, string, string, *model.Task, string) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"native":true}`))}, nil
}
func (a *terminalIdentityAdaptor) ParseTaskResult(_ *model.Task, _ *http.Response, _ []byte) (*relaycommon.TaskInfo, error) {
	id := a.initChannelMeta().ChannelId
	if id == 47 {
		return &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "channel47 failed"}, nil
	}
	return &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Url: "https://example.com/channel48", Progress: "100%"}, nil
}
func (a *terminalIdentityAdaptor) AdjustBillingOnComplete(_ *model.Task, result *relaycommon.TaskInfo) int {
	if result.Status == model.TaskStatusFailure {
		return 0
	}
	return 500
}

func TestTaskPollingChannelIdentitySettlesEachWalletOnce(t *testing.T) {
	truncate(t)
	oldFactory, oldCache := GetTaskAdaptorFunc, common.MemoryCacheEnabled
	oldTimeout, oldLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
	t.Cleanup(func() {
		GetTaskAdaptorFunc = oldFactory
		common.MemoryCacheEnabled = oldCache
		constant.TaskTimeoutMinutes = oldTimeout
		constant.TaskQueryLimit = oldLimit
	})
	common.MemoryCacheEnabled = false
	constant.TaskTimeoutMinutes = 0
	constant.TaskQueryLimit = 100
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return &terminalIdentityAdaptor{} }
	for _, id := range []int{47, 48} {
		seedChannel(t, id)
		require.NoError(t, model.DB.Create(&model.User{Id: id, Username: fmt.Sprintf("identity_user_%d", id), AffCode: fmt.Sprintf("identity_aff_%d", id), Quota: 5000, Status: common.UserStatusEnabled}).Error)
		task := makeTask(id, id, 1000, 0, BillingSourceWallet)
		task.TaskID = fmt.Sprintf("terminal_identity_%d", id)
		task.Platform = constant.TaskPlatform("kling")
		task.SubmitTime = time.Now().Unix()
		task.PrivateData.UpstreamTaskID = "shared_terminal_id"
		require.NoError(t, model.DB.Create(task).Error)
	}
	require.Equal(t, 2, RunTaskPollingOnce(context.Background(), nil).UnfinishedTasks)
	for i := 0; i < 2; i++ {
		require.Zero(t, RunTaskPollingOnce(context.Background(), nil).UnfinishedTasks)
	}
	var failed, success model.Task
	require.NoError(t, model.DB.Where("channel_id = ?", 47).First(&failed).Error)
	require.NoError(t, model.DB.Where("channel_id = ?", 48).First(&success).Error)
	require.EqualValues(t, model.TaskStatusFailure, failed.Status)
	require.Equal(t, "channel47 failed", failed.FailReason)
	require.Zero(t, failed.Quota)
	require.EqualValues(t, model.TaskStatusSuccess, success.Status)
	require.Equal(t, "https://example.com/channel48", success.GetResultURL())
	require.Equal(t, 500, success.Quota)
	require.Equal(t, 6000, getUserQuota(t, 47))
	require.Equal(t, 5500, getUserQuota(t, 48))
}
