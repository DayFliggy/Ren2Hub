package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunTaskPollingOnceContinuesAtFullProgress(t *testing.T) {
	previousLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = previousLimit })
	for _, action := range []string{constant.TaskActionTextToVideo, constant.TaskActionImageToVideo} {
		for _, progress := range []string{"99%", "100%"} {
			for _, terminal := range []string{model.TaskStatusSuccess, model.TaskStatusFailure} {
				t.Run(action+"/"+progress+"/"+terminal, func(t *testing.T) {
					truncate(t)
					const channelID, userID, initialQuota, taskQuota = 405, 405, 10_000, 1_200
					seedUser(t, userID, initialQuota)
					seedTaskPollingChannel(t, channelID, true)
					task := seedPollingTask(t, channelID, "task_progress", "upstream_progress")
					task.Action = action
					task.UserId = userID
					task.Quota = taskQuota
					task.SubmitTime = time.Now().Unix()
					require.NoError(t, model.DB.Save(task).Error)
					adaptor := &scriptedPollingAdaptor{parse: &relaycommon.TaskInfo{
						TaskID: task.GetUpstreamTaskID(), Status: model.TaskStatusInProgress, Progress: progress,
					}}
					previousFactory := GetTaskAdaptorFunc
					GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
					t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

					assert.Equal(t, 1, RunTaskPollingOnce(context.Background(), nil).UnfinishedTasks)
					var persisted model.Task
					require.NoError(t, model.DB.First(&persisted, task.ID).Error)
					assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status)
					assert.Equal(t, progress, persisted.Progress)
					assert.Zero(t, persisted.FinishTime)
					assert.Equal(t, initialQuota, getUserQuota(t, userID))
					assert.Zero(t, countLogs(t))
					require.True(t, model.HasUnfinishedSyncTasks(), "the scheduler must run again even at 100%")

					adaptor.parse = &relaycommon.TaskInfo{TaskID: task.GetUpstreamTaskID(), Status: terminal, Progress: "100%"}
					if terminal == model.TaskStatusFailure {
						adaptor.parse.Reason = "upstream finalization failed"
					}
					assert.Equal(t, 1, RunTaskPollingOnce(context.Background(), nil).UnfinishedTasks)
					require.NoError(t, model.DB.First(&persisted, task.ID).Error)
					assert.EqualValues(t, terminal, persisted.Status)
					assert.NotZero(t, persisted.FinishTime)
					if terminal == model.TaskStatusFailure {
						assert.Zero(t, persisted.Quota)
						assert.Equal(t, initialQuota+taskQuota, getUserQuota(t, userID))
						assert.EqualValues(t, 1, countLogs(t))
					} else {
						assert.Equal(t, taskQuota, persisted.Quota)
						assert.Equal(t, initialQuota, getUserQuota(t, userID))
						assert.Zero(t, countLogs(t))
					}
					quota, logs := getUserQuota(t, userID), countLogs(t)
					assert.False(t, model.HasUnfinishedSyncTasks())
					assert.Zero(t, RunTaskPollingOnce(context.Background(), nil).UnfinishedTasks)
					assert.Equal(t, quota, getUserQuota(t, userID))
					assert.Equal(t, logs, countLogs(t), "terminal tasks must not be settled or refunded again")
				})
			}
		}
	}
}

func TestSweepTimedOutFullProgressTaskRefundsOnce(t *testing.T) {
	truncate(t)
	const userID, initialQuota, taskQuota = 405, 10000, 1200
	seedUser(t, userID, initialQuota)
	task := makeTask(userID, 0, taskQuota, 0, BillingSourceWallet)
	task.TaskID = "full_progress_before_upgrade"
	task.Progress = "100%"
	task.SubmitTime = time.Now().Add(-73 * time.Hour).Unix()
	require.NoError(t, model.DB.Create(task).Error)
	old := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 72 * 60
	t.Cleanup(func() { constant.TaskTimeoutMinutes = old })
	for i := 0; i < 2; i++ {
		sweepTimedOutTasks(context.Background())
		var stored model.Task
		require.NoError(t, model.DB.First(&stored, task.ID).Error)
		require.EqualValues(t, model.TaskStatusFailure, stored.Status)
		require.Zero(t, stored.Quota)
		require.Equal(t, initialQuota+taskQuota, getUserQuota(t, userID))
		require.EqualValues(t, 1, countLogs(t))
	}
}
