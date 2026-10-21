package service

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// capturePollSkipLog 重置限频状态并截获 WARN 输出。
func capturePollSkipLog(t *testing.T) *lockedLogBuffer {
	t.Helper()
	pollSkipLog.mu.Lock()
	pollSkipLog.last, pollSkipLog.suppressed = time.Time{}, 0
	pollSkipLog.mu.Unlock()
	captured := &lockedLogBuffer{}
	common.LogWriterMu.Lock()
	previous := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = captured
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultErrorWriter = previous
		common.LogWriterMu.Unlock()
	})
	return captured
}

func TestTaskPollAtChannelLimitSkipsWithoutFailureAndLogsOnce(t *testing.T) {
	truncate(t)
	const userID, tokenID, channelID = 710, 710, 710
	const initialQuota, preConsumed, tokenRemain = 10_000, 4_000, 7_000
	seedUser(t, userID, initialQuota)
	seedToken(t, tokenID, userID, "sk-poll-full", tokenRemain)
	ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeKling, Name: "poll-full", Key: "sk-test", Status: common.ChannelStatusEnabled, MaxConcurrency: 1}
	require.NoError(t, model.DB.Create(ch).Error)
	task := makeTask(userID, channelID, preConsumed, tokenID, BillingSourceWallet)
	task.TaskID = "task_poll_full"
	task.PrivateData.UpstreamTaskID = "upstream_poll_full"
	require.NoError(t, model.DB.Create(task).Error)
	upstreamID := task.GetUpstreamTaskID()
	taskM := map[string]*model.Task{upstreamID: task}

	// 一个中继请求占住渠道唯一的并发许可。
	permit, err := AcquireChannelConcurrency(context.Background(), ch)
	require.NoError(t, err)
	t.Cleanup(permit.Release)
	logs := capturePollSkipLog(t)

	single := &taskPollingFetchAdaptor{}
	require.NoError(t, updateVideoSingleTask(context.Background(), single, ch, upstreamID, taskM))
	require.NoError(t, updateVideoSingleTask(context.Background(), single, ch, upstreamID, taskM))
	batch := &batchPollingAdaptor{}
	require.NoError(t, UpdateBatchTasks(context.Background(), batch, map[int][]string{channelID: {upstreamID}}, taskM))

	assert.Zero(t, single.fetchCount(), "a full channel must not be polled")
	assert.Zero(t, batch.batchCalls, "a full channel must not be polled")
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.Zero(t, persisted.PrivateData.PollFailures, "skipping a full channel is not a poll failure")
	assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Equal(t, tokenRemain, getTokenRemainQuota(t, tokenID))
	assert.Equal(t, 1, strings.Count(logs.String(), "task poll skipped"), "skips are logged at most once per interval")

	// 许可释放后，下一轮照常轮询。
	permit.Release()
	require.NoError(t, updateVideoSingleTask(context.Background(), single, ch, upstreamID, taskM))
	assert.Equal(t, 1, single.fetchCount())
}
