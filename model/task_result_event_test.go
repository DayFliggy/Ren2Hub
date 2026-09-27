package model

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskResultEventSurvivesReconciliationAndMetricDeliveryRetry(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		ctx := context.Background()
		user, token, _, task, _ := newAcceptedTaskFixture(t, db, "terminal-evidence")
		task.Properties.OriginModelName = "result-model"
		task.PrivateData.PluginRef = &TaskPluginRef{Key: "pinned-plugin", Version: "1.0.0", SourceHash: strings.Repeat("a", 64)}
		require.NoError(t, db.Model(&Task{}).Where("id = ?", task.ID).Updates(map[string]any{"private_data": task.PrivateData, "properties": task.Properties}).Error)
		from := task.Status
		task.Status, task.Progress = TaskStatusSuccess, "100%"
		task.SubmitTime, task.StartTime, task.FinishTime = 7200, 7210, 7230
		task.Data = []byte(`{"output":"completed"}`)
		task.PrivateData.PluginState = []byte(`{"cursor":"final"}`)
		task.PrivateData.CompletionUsage = &TaskCompletionUsage{Facts: map[string]any{"seconds": 10.0}, Error: "price requires an unavailable fact"}
		for range 2 {
			require.NoError(t, HoldTaskBilling(ctx, &task, from, "task_final_usage_invalid"))
		}
		var persisted Task
		require.NoError(t, db.First(&persisted, task.ID).Error)
		assert.EqualValues(t, TaskStatusSuccess, persisted.Status)
		assert.EqualValues(t, 7230, persisted.FinishTime)
		assert.JSONEq(t, string(task.PrivateData.PluginState), string(persisted.PrivateData.PluginState))
		assert.Equal(t, task.PrivateData.PluginRef, persisted.PrivateData.PluginRef)
		assert.Equal(t, task.PrivateData.CompletionUsage, persisted.PrivateData.CompletionUsage)
		assertTaskAccountProjection(t, db, user.Id, token.Id, 60, 40, 0)
		var event TaskResultEvent
		require.NoError(t, db.First(&event, task.ID).Error)
		assert.Equal(t, "1.0.0", event.PluginVersion)
		assert.EqualValues(t, 7230, event.FinishTime)
		assert.EqualValues(t, 7200, event.BucketTs)
		assert.Equal(t, BillingRecoveryStatePending, event.MetricsState)
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("result_marker_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "task_result_events" {
				tx.AddError(assert.AnError)
			}
		}))
		require.ErrorIs(t, DeliverTaskResultMetrics(ctx, task.ID), assert.AnError)
		require.NoError(t, db.Callback().Update().Remove("result_marker_failure"))
		var count int64
		require.NoError(t, db.Model(&PerfMetric{}).Count(&count).Error)
		assert.Zero(t, count, "metric write rolls back with failed delivery marker")
		var group sync.WaitGroup
		errors := make(chan error, 2)
		for range 2 {
			group.Add(1)
			go func() { defer group.Done(); errors <- DeliverTaskResultMetrics(ctx, task.ID) }()
		}
		group.Wait()
		close(errors)
		for err := range errors {
			require.NoError(t, err)
		}
		require.NoError(t, RecoverTaskResultMetrics(ctx, 10))
		var metric PerfMetric
		require.NoError(t, db.First(&metric).Error)
		assert.EqualValues(t, 1, metric.RequestCount)
		assert.EqualValues(t, 1, metric.SuccessCount)
		assert.EqualValues(t, 30000, metric.TotalLatencyMs)
		assert.EqualValues(t, 20000, metric.GenerationMs)
		assert.Zero(t, metric.OutputTokens, "invalid usage is not published as token evidence")
		var recovery BillingRecovery
		require.NoError(t, db.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
		command := BillingResolution{OperationID: "terminal-evidence-resolution", ExpectedVersion: recovery.Version, Action: "settle", Quota: 15, Reason: "verified provider usage"}
		_, err := ResolveBillingReconciliation(ctx, recovery.ID, 1, command)
		require.Error(t, err, "a reason and amount do not replace missing actual usage")
		assertTaskAccountProjection(t, db, user.Id, token.Id, 60, 40, 0)
		command.Action, command.Quota, command.Reason = "release", 0, "explicitly waive charge because provider usage is unavailable"
		for range 2 {
			_, err := ResolveBillingReconciliation(ctx, recovery.ID, 1, command)
			require.NoError(t, err)
			require.NoError(t, ApplyBillingRecoveryConsumption(recovery.RequestID))
			require.NoError(t, RecoverTaskResultMetrics(ctx, 10))
		}
		assertTaskAccountProjection(t, db, user.Id, token.Id, 100, 0, 0)
		require.NoError(t, db.First(&persisted, task.ID).Error)
		assert.Zero(t, persisted.Quota)
		assert.EqualValues(t, 7230, persisted.FinishTime)
		require.NoError(t, db.First(&metric, metric.Id).Error)
		assert.EqualValues(t, 1, metric.RequestCount)
		require.NoError(t, db.Model(&TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&count).Error)
		assert.EqualValues(t, 1, count)
	})
}

func TestTaskTerminalRejectsChangedPinnedIdentity(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		for _, mode := range []string{"completion", "hold"} {
			t.Run(mode, func(t *testing.T) {
				_, _, _, task, consumption := newAcceptedTaskFixture(t, db, "identity-"+mode)
				task.PrivateData.PluginRef = &TaskPluginRef{Key: "pinned-plugin", Version: "1.0.0", SourceHash: strings.Repeat("a", 64)}
				require.NoError(t, db.Model(&Task{}).Where("id = ?", task.ID).Update("private_data", task.PrivateData).Error)
				from := task.Status
				task.Status, task.Progress = TaskStatusSuccess, "100%"
				task.PrivateData.PluginRef.Version = "2.0.0"
				if mode == "completion" {
					won, err := PersistTaskCompletion(context.Background(), &task, from, 15, consumption)
					require.ErrorIs(t, err, ErrBillingRecoveryStateConflict)
					assert.False(t, won)
				} else {
					require.ErrorIs(t, HoldTaskBilling(context.Background(), &task, from, "invalid_usage"), ErrBillingRecoveryStateConflict)
				}
				var persisted Task
				require.NoError(t, db.First(&persisted, task.ID).Error)
				assert.Equal(t, from, persisted.Status)
				var count int64
				require.NoError(t, db.Model(&TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&count).Error)
				assert.Zero(t, count)
			})
		}
	})
}
