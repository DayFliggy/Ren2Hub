package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scriptedPollAdaptor answers every single-task poll with one fixed HTTP
// response; the legacy ParseTaskResult hook turns the body into a status.
type scriptedPollAdaptor struct {
	status int
	body   string
}

func (a *scriptedPollAdaptor) Init(*relaycommon.RelayInfo) {}

func (a *scriptedPollAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader(a.body))}, nil
}

func (a *scriptedPollAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	var info relaycommon.TaskInfo
	if err := common.Unmarshal(body, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func seedFailurePollTask(t *testing.T, userID, channelID int) *model.Task {
	t.Helper()
	seedUser(t, userID, 10_000)
	seedTaskPollingChannel(t, channelID, true)
	task := makeTask(userID, channelID, 1_200, 0, BillingSourceWallet)
	task.TaskID = fmt.Sprintf("task_poll_failure_%d", channelID)
	task.Platform = constant.TaskPlatform("kling")
	acceptTestTask(t, task)
	return task
}

func pollOnce(t *testing.T, adaptor TaskPollingAdaptor, task *model.Task) error {
	t.Helper()
	var current model.Task
	require.NoError(t, model.DB.First(&current, task.ID).Error)
	upstreamID := current.GetUpstreamTaskID()
	return updateVideoSingleTask(context.Background(), adaptor, &model.Channel{Id: current.ChannelId}, upstreamID, map[string]*model.Task{upstreamID: &current})
}

func reloadTask(t *testing.T, id int64) model.Task {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.First(&task, id).Error)
	return task
}

func TestPollMissingUpstreamTaskReleasesReservationOnce(t *testing.T) {
	truncate(t)
	task := seedFailurePollTask(t, 501, 501)
	adaptor := &scriptedPollAdaptor{status: http.StatusNotFound, body: `{}`}

	require.Error(t, pollOnce(t, adaptor, task))
	stored := reloadTask(t, task.ID)
	assert.EqualValues(t, model.TaskStatusFailure, stored.Status)
	assert.Equal(t, "poll failed: not_found (HTTP 404)", stored.FailReason)
	assert.Zero(t, stored.Quota)
	assert.Equal(t, 10_000, getUserQuota(t, 501))

	// A stale poll of the finished task must not release twice.
	_ = updateVideoSingleTask(context.Background(), adaptor, &model.Channel{Id: 501}, task.GetUpstreamTaskID(), map[string]*model.Task{task.GetUpstreamTaskID(): task})
	assert.Equal(t, 10_000, getUserQuota(t, 501))
}

func TestPollTransientFailuresStopAtLimitAndRelease(t *testing.T) {
	truncate(t)
	previous := constant.TaskPollMaxFailures
	constant.TaskPollMaxFailures = 3
	t.Cleanup(func() { constant.TaskPollMaxFailures = previous })
	task := seedFailurePollTask(t, 502, 502)
	adaptor := &scriptedPollAdaptor{status: http.StatusServiceUnavailable, body: `upstream down`}

	for attempt := 1; attempt < 3; attempt++ {
		require.Error(t, pollOnce(t, adaptor, task))
		stored := reloadTask(t, task.ID)
		assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
		assert.Equal(t, attempt, stored.PrivateData.PollFailures)
	}
	require.Error(t, pollOnce(t, adaptor, task))
	stored := reloadTask(t, task.ID)
	assert.EqualValues(t, model.TaskStatusFailure, stored.Status)
	assert.Equal(t, "poll failed: transient (HTTP 503)", stored.FailReason)
	assert.Zero(t, stored.Quota)
	assert.Equal(t, 10_000, getUserQuota(t, 502))
}

func TestPollRecognizedRunningResultResetsFailures(t *testing.T) {
	truncate(t)
	task := seedFailurePollTask(t, 503, 503)

	require.Error(t, pollOnce(t, &scriptedPollAdaptor{status: http.StatusOK, body: `{"status":"WEIRD"}`}, task))
	assert.Equal(t, 1, reloadTask(t, task.ID).PrivateData.PollFailures)

	require.NoError(t, pollOnce(t, &scriptedPollAdaptor{status: http.StatusOK, body: `{"status":"IN_PROGRESS","progress":"100%"}`}, task))
	stored := reloadTask(t, task.ID)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.Zero(t, stored.PrivateData.PollFailures)
	// A running job never reads as complete, and it stays in the polling set.
	assert.NotEqual(t, "100%", stored.Progress)
	unfinished := model.GetAllUnFinishSyncTasks(10)
	require.Len(t, unfinished, 1)
	assert.Equal(t, task.ID, unfinished[0].ID)
}

func TestPollProviderErrorReportingRunningJobCountsAsFailure(t *testing.T) {
	truncate(t)
	task := seedFailurePollTask(t, 504, 504)

	require.Error(t, pollOnce(t, &scriptedPollAdaptor{status: http.StatusBadRequest, body: `{"status":"IN_PROGRESS"}`}, task))
	stored := reloadTask(t, task.ID)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.Equal(t, 1, stored.PrivateData.PollFailures)
}

func TestPollCancellationDoesNotCountAsProviderFailure(t *testing.T) {
	truncate(t)
	task := seedFailurePollTask(t, 507, 507)
	task.PrivateData.PollFailures = 2
	require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := failTaskPolls(ctx, []*model.Task{task}, pollClassTransient, http.StatusServiceUnavailable, errors.New("runner stopped"))
	require.ErrorIs(t, err, context.Canceled)
	stored := reloadTask(t, task.ID)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.Equal(t, 2, stored.PrivateData.PollFailures)
}

// A running immediate result is accepted as an asynchronous task: the
// reservation stays held and polling picks the task up, as upstream does.
func TestRunningImmediateResultBecomesPollableTask(t *testing.T) {
	truncate(t)
	seedUser(t, 506, 10_000)
	task := makeTask(506, 0, 900, 0, BillingSourceWallet)
	task.TaskID = "task_running_immediate"
	task.Status, task.Progress = model.TaskStatusNotStart, "0%"
	ApplyRunningImmediateResult(task, &relaycommon.TaskInfo{Status: model.TaskStatusInProgress, PluginState: []byte(`{"cursor":1}`)})
	acceptTestTask(t, task)

	stored := reloadTask(t, task.ID)
	assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
	assert.JSONEq(t, `{"cursor":1}`, string(stored.PrivateData.PluginState))
	assert.EqualValues(t, 900, stored.Quota)
	unfinished := model.GetAllUnFinishSyncTasks(10)
	require.Len(t, unfinished, 1)
	assert.Equal(t, task.ID, unfinished[0].ID)
}

// batchPollAdaptor records batch requests and reports each requested task
// except the ones listed in omit.
type batchPollAdaptor struct {
	mu         sync.Mutex
	batchCalls [][]string
	omit       map[string]bool
}

func (a *batchPollAdaptor) Init(*relaycommon.RelayInfo) {}
func (a *batchPollAdaptor) FetchTask(string, string, map[string]any, string) (*http.Response, error) {
	return nil, assert.AnError
}
func (a *batchPollAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) { return nil, nil }
func (a *batchPollAdaptor) FetchMode() string                                     { return "batch" }

func (a *batchPollAdaptor) FetchBatchTasks(_ string, _ string, tasks []*model.Task, _ string) (*http.Response, error) {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.GetUpstreamTaskID())
	}
	a.mu.Lock()
	a.batchCalls = append(a.batchCalls, ids)
	a.mu.Unlock()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader([]byte(`{}`)))}, nil
}

func (a *batchPollAdaptor) ParseBatchResult(tasks []*model.Task, _ *http.Response, _ []byte) (map[string]*BatchTaskResult, error) {
	results := make(map[string]*BatchTaskResult)
	for _, task := range tasks {
		id := task.GetUpstreamTaskID()
		if a.omit[id] {
			continue
		}
		results[id] = &BatchTaskResult{TaskInfo: relaycommon.TaskInfo{TaskID: id, Status: model.TaskStatusInProgress, Progress: "45%"}}
	}
	return results, nil
}

func TestBatchPluginTasksShareOneProviderRequest(t *testing.T) {
	truncate(t)
	const channelID = 505
	seedTaskPollingChannel(t, channelID, true)
	seedUser(t, 505, 10_000)
	ref := &hosttypes.TaskPluginRef{Key: "batchy", Version: "1.0.0", SourceHash: strings.Repeat("a", 64)}
	var tasks []*model.Task
	taskM := make(map[string]*model.Task)
	var ids []string
	for _, name := range []string{"one", "two", "gone"} {
		task := makeTask(505, channelID, 100, 0, BillingSourceWallet)
		task.TaskID = "task_batch_" + name
		task.Platform = constant.TaskPlatform("batchy")
		acceptTestTask(t, task)
		task.PrivateData.PluginRef = ref
		require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
		tasks = append(tasks, task)
		taskM[task.GetUpstreamTaskID()] = task
		ids = append(ids, task.GetUpstreamTaskID())
	}
	adaptor := &batchPollAdaptor{omit: map[string]bool{tasks[2].GetUpstreamTaskID(): true}}
	previousPinned, previousFactory := GetPinnedTaskAdaptorFunc, GetTaskAdaptorFunc
	GetPinnedTaskAdaptorFunc = func(*model.Task) (TaskPollingAdaptor, error) { return adaptor, nil }
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetPinnedTaskAdaptorFunc, GetTaskAdaptorFunc = previousPinned, previousFactory })

	require.NoError(t, UpdateVideoTasks(context.Background(), constant.TaskPlatform("batchy"), map[int][]string{channelID: ids}, taskM))

	require.Len(t, adaptor.batchCalls, 1)
	assert.ElementsMatch(t, ids, adaptor.batchCalls[0])
	for _, task := range tasks[:2] {
		stored := reloadTask(t, task.ID)
		assert.Equal(t, "45%", stored.Progress)
		assert.Zero(t, stored.PrivateData.PollFailures)
	}
	missing := reloadTask(t, tasks[2].ID)
	assert.EqualValues(t, model.TaskStatusInProgress, missing.Status)
	assert.Equal(t, 1, missing.PrivateData.PollFailures)
}
