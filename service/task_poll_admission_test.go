package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// erroringPollAdaptor fails every send after admission, like a vendor timeout.
type erroringPollAdaptor struct {
	batchPollingAdaptor
	sends int
}

func (a *erroringPollAdaptor) FetchTask(context.Context, string, string, *model.Task, string) (*http.Response, error) {
	a.sends++
	return nil, fmt.Errorf("vendor timeout: %w", context.DeadlineExceeded)
}

func (a *erroringPollAdaptor) FetchBatchTasks(context.Context, string, string, []*model.Task, string) (*http.Response, error) {
	a.sends++
	return nil, fmt.Errorf("vendor timeout: %w", context.DeadlineExceeded)
}

// onChannelRead runs fn before matching channel reads on the shared test DB.
func onChannelRead(t *testing.T, name string, match func(*gorm.Statement) bool, fn func(*gorm.DB)) {
	t.Helper()
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" && match(tx.Statement) {
			fn(tx)
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Query().Remove(name)) })
}

func isLimitRead(statement *gorm.Statement) bool {
	return slices.Contains(statement.Selects, "max_concurrency")
}

// isDeletionCheck matches the concurrency admission confirmation read.
func isDeletionCheck(statement *gorm.Statement) bool {
	return slices.Contains(statement.Omits, "key")
}

// Admission failures of a task poll are not upstream poll failures (audit B8).
// The fixture is one failure short of the give-up threshold, so counting such a
// failure would end the task as "upstream query failed" and refund even a
// legacy task, which the missing-channel flow never refunds.
func TestTaskPollAdmissionFailuresAreNotUpstreamFailures(t *testing.T) {
	previousMax, previousCache := constant.TaskPollMaxFailures, common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { constant.TaskPollMaxFailures, common.MemoryCacheEnabled = previousMax, previousCache })
	const quota, initialUserQuota, presetFailures = 1_200, 10_000, 19

	scenarios := []struct {
		name   string
		inject func(t *testing.T, channelID int)
	}{
		{"limit read misses a channel the database still has", func(t *testing.T, _ int) {
			onChannelRead(t, "test:poll-not-found", isLimitRead, func(tx *gorm.DB) { tx.AddError(gorm.ErrRecordNotFound) })
		}},
		{"deletion cannot be confirmed", func(t *testing.T, _ int) {
			onChannelRead(t, "test:poll-not-found", isLimitRead, func(tx *gorm.DB) { tx.AddError(gorm.ErrRecordNotFound) })
			onChannelRead(t, "test:poll-confirm-failure", isDeletionCheck, func(tx *gorm.DB) { tx.AddError(errors.New("confirmation read failure")) })
		}},
		{"limit unreadable", func(t *testing.T, _ int) {
			onChannelRead(t, "test:poll-limit-failure", isLimitRead, func(tx *gorm.DB) { tx.AddError(errors.New("limit read failure")) })
		}},
	}
	id := 7300
	for _, threshold := range []int{20, 0} {
		for _, scenario := range scenarios {
			for _, batch := range []bool{false, true} {
				for _, legacy := range []bool{false, true} {
					id++
					channelID := id
					name := fmt.Sprintf("threshold=%d/%s/batch=%t/legacy=%t", threshold, scenario.name, batch, legacy)
					t.Run(name, func(t *testing.T) {
						truncate(t)
						constant.TaskPollMaxFailures = threshold
						seedUser(t, channelID, initialUserQuota)
						ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeKling, Name: "poll-admission", Key: "sk-test", Status: common.ChannelStatusEnabled}
						require.NoError(t, model.DB.Create(ch).Error)
						task := makeTask(channelID, channelID, quota, 0, BillingSourceWallet)
						task.TaskID = fmt.Sprintf("task_poll_admission_%d", channelID)
						task.PrivateData.UpstreamTaskID = fmt.Sprintf("upstream_poll_admission_%d", channelID)
						task.PrivateData.PollFailures = presetFailures
						task.SubmitTime = model.TaskRefundLegacyCutoff
						if legacy {
							task.SubmitTime = model.TaskRefundLegacyCutoff - 1
						}
						require.NoError(t, model.DB.Create(task).Error)
						ids := []string{task.GetUpstreamTaskID()}
						taskM := map[string]*model.Task{ids[0]: task}
						gatewayConcurrency.forget("channel", []int{channelID})
						scenario.inject(t, channelID)

						adaptor := &erroringPollAdaptor{}
						if batch {
							_ = updateBatchTasks(context.Background(), adaptor, channelID, ids, taskM)
						} else {
							_ = updateVideoSingleTask(context.Background(), adaptor, ch, ids[0], taskM)
						}

						assert.Zero(t, adaptor.sends, "admission failed, so nothing is sent")
						var stored model.Task
						require.NoError(t, model.DB.First(&stored, task.ID).Error)
						assert.Equal(t, presetFailures, stored.PrivateData.PollFailures, "admission failures are not counted")
						assert.EqualValues(t, model.TaskStatusInProgress, stored.Status)
						assert.Equal(t, quota, stored.Quota)
						assert.Equal(t, initialUserQuota, getUserQuota(t, channelID))
					})
				}
			}
		}
	}
	for _, batch := range []bool{false, true} {
		id++
		channelID := id
		t.Run(fmt.Sprintf("send timeout still counts/batch=%t", batch), func(t *testing.T) {
			truncate(t)
			constant.TaskPollMaxFailures = 20
			seedUser(t, channelID, initialUserQuota)
			ch := &model.Channel{Id: channelID, Type: constant.ChannelTypeKling, Name: "poll-timeout", Key: "sk-test", Status: common.ChannelStatusEnabled}
			require.NoError(t, model.DB.Create(ch).Error)
			task := makeTask(channelID, channelID, quota, 0, BillingSourceWallet)
			task.TaskID = fmt.Sprintf("task_poll_timeout_%d", channelID)
			task.PrivateData.UpstreamTaskID = fmt.Sprintf("upstream_poll_timeout_%d", channelID)
			task.PrivateData.PollFailures = presetFailures
			task.SubmitTime = model.TaskRefundLegacyCutoff
			require.NoError(t, model.DB.Create(task).Error)
			ids := []string{task.GetUpstreamTaskID()}
			taskM := map[string]*model.Task{ids[0]: task}

			adaptor := &erroringPollAdaptor{}
			if batch {
				_ = updateBatchTasks(context.Background(), adaptor, channelID, ids, taskM)
			} else {
				_ = updateVideoSingleTask(context.Background(), adaptor, ch, ids[0], taskM)
			}

			assert.Equal(t, 1, adaptor.sends)
			var stored model.Task
			require.NoError(t, model.DB.First(&stored, task.ID).Error)
			assert.Equal(t, presetFailures+1, stored.PrivateData.PollFailures)
			assert.EqualValues(t, model.TaskStatusFailure, stored.Status)
			assert.Equal(t, "Failed to query the upstream task status", stored.FailReason)
		})
	}
}

// 渠道在读取并发额度时删除，属于本地准入失败；不得把它结算为供应商失败。
func TestTaskPollAdmissionDeletionSkipsWithoutSideEffects(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(fmt.Sprintf("batch=%t", batch), func(t *testing.T) {
			truncate(t)
			const id = 7901
			oldCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = false
			t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })
			seedUser(t, id, 8800)
			seedToken(t, id, id, "admission-deletion-test", 4200)
			ch := &model.Channel{Id: id, Type: constant.ChannelTypeKling, Name: "admission-deletion", Key: "test", Status: common.ChannelStatusEnabled}
			require.NoError(t, model.DB.Create(ch).Error)
			task := makeTask(id, id, 1200, id, BillingSourceWallet)
			task.TaskID = "admission-deletion"
			task.PrivateData.UpstreamTaskID = "admission-deletion-upstream"
			task.PrivateData.PollFailures = 19
			require.NoError(t, model.DB.Create(task).Error)
			var before model.Task
			require.NoError(t, model.DB.First(&before, task.ID).Error)
			observationsBefore := routeObservations.snapshot()
			healthBefore, weightBefore := routeObservations.routeHealth(id, "test-model")
			logsBefore := countLogs(t)
			gatewayConcurrency.forget("channel", []int{id})
			onChannelRead(t, "test:admission-deletion", isLimitRead, func(tx *gorm.DB) {
				require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).Delete(&model.Channel{}, id).Error)
			})
			adaptor := &erroringPollAdaptor{}
			tasks := map[string]*model.Task{task.GetUpstreamTaskID(): task}
			if batch {
				require.NoError(t, updateBatchTasks(context.Background(), adaptor, id, []string{task.GetUpstreamTaskID()}, tasks))
			} else {
				require.NoError(t, updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), tasks))
			}
			var after model.Task
			require.NoError(t, model.DB.First(&after, task.ID).Error)
			assert.Equal(t, before, after)
			assert.Equal(t, 8800, getUserQuota(t, id))
			assert.Equal(t, 4200, getTokenRemainQuota(t, id))
			assert.Equal(t, logsBefore, countLogs(t))
			assert.Zero(t, adaptor.sends)
			assert.Zero(t, ChannelInFlight(id))
			assert.Equal(t, observationsBefore, routeObservations.snapshot())
			healthAfter, weightAfter := routeObservations.routeHealth(id, "test-model")
			assert.Equal(t, healthBefore, healthAfter)
			assert.Equal(t, weightBefore, weightAfter)
		})
	}
}
