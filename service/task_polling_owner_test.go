package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ownedSunoPollingAdaptor struct {
	mu    sync.Mutex
	calls map[string][]string
}

func (a *ownedSunoPollingAdaptor) Init(*relaycommon.RelayInfo) {}
func (a *ownedSunoPollingAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return nil, nil
}
func (a *ownedSunoPollingAdaptor) AdjustBillingOnComplete(*model.Task, *relaycommon.TaskInfo) int {
	return 0
}
func (a *ownedSunoPollingAdaptor) FetchTask(_ string, key string, body map[string]any, _ string) (*http.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids, _ := body["ids"].([]string)
	a.calls[key] = append(a.calls[key], ids...)
	items := make([]taskdto.SunoDataResponse, 0, len(ids))
	for _, id := range ids {
		data, err := common.Marshal(map[string]string{"owner": key})
		if err != nil {
			return nil, err
		}
		items = append(items, taskdto.SunoDataResponse{TaskID: id, Status: string(model.TaskStatusInProgress), Data: data})
	}
	data, err := common.Marshal(taskdto.TaskResponse[[]taskdto.SunoDataResponse]{Code: taskdto.TaskSuccessCode, Data: items})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data))}, nil
}

func TestTaskPollingScopesDuplicateProviderIDsByCredential(t *testing.T) {
	truncate(t)
	previousLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = previousLimit })
	require.NoError(t, model.DB.AutoMigrate(&model.GatewayCredential{}))
	ctx := context.Background()
	baseURL := "https://polling.example.invalid"
	channel := &model.Channel{Name: "polling-owner", Type: constant.ChannelTypeSunoAPI, Status: common.ChannelStatusEnabled,
		Key: "first\nsecond", BaseURL: &baseURL, ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}}
	require.NoError(t, model.DB.Create(channel).Error)
	other := &model.Channel{Name: "polling-other", Type: constant.ChannelTypeSunoAPI, Status: common.ChannelStatusEnabled, Key: "third", BaseURL: &baseURL}
	require.NoError(t, model.DB.Create(other).Error)
	seedUser(t, 1, 10000)
	tasks := make([]*model.Task, 0, 3)
	for index, key := range []string{"first", "second", "third"} {
		owner := channel
		if key == "third" {
			owner = other
		}
		task := &model.Task{TaskID: fmt.Sprintf("task_owner_%d", index), UserId: 1, ChannelId: owner.Id,
			Platform: constant.TaskPlatformSuno, Status: model.TaskStatusNotStart, Progress: "0%", SubmitTime: time.Now().Unix()}
		require.NoError(t, task.PrivateData.BindCredential(ctx, owner, key))
		task.PrivateData.UpstreamTaskID = "same-provider-id"
		acceptTestTask(t, task)
		tasks = append(tasks, task)
	}
	require.NoError(t, model.DB.Model(channel).Update("key", "second\nfirst").Error)
	adaptor := &ownedSunoPollingAdaptor{calls: make(map[string][]string)}
	previous := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previous })
	summary := RunTaskPollingOnce(ctx, nil)
	assert.Equal(t, 1, summary.PlatformsScanned)
	for index, key := range []string{"first", "second", "third"} {
		assert.Equal(t, []string{"same-provider-id"}, adaptor.calls[key])
		var stored model.Task
		require.NoError(t, model.DB.First(&stored, tasks[index].ID).Error)
		assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)
		assert.JSONEq(t, fmt.Sprintf(`{"owner":%q}`, key), string(stored.Data))
	}
	require.NoError(t, model.DB.Model(other).Update("key", "replacement").Error)
	RunTaskPollingOnce(ctx, nil)
	assert.Empty(t, adaptor.calls["replacement"])
	assert.Len(t, adaptor.calls["third"], 1)
	var unavailable model.Task
	require.NoError(t, model.DB.First(&unavailable, tasks[2].ID).Error)
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), unavailable.Status, "unavailable credentials do not turn provider jobs into failures")
}
