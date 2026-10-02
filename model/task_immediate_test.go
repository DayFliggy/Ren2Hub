package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestImmediateTaskAtomicCompletionAcrossDatabases(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		for _, discard := range []bool{false, true} {
			t.Run(fmt.Sprintf("discard=%t", discard), func(t *testing.T) {
				id := fmt.Sprintf("immediate-%t", discard)
				user, token, _, task, consumption := newTaskBillingFixture(t, db, id)
				task.Status, task.Progress, task.FinishTime = TaskStatusSuccess, "100%", 7230
				task.Data = []byte(`{"result":"completed"}`)
				task.PrivateData.ResultURL = "https://example.invalid/result"
				task.PrivateData.PluginState = []byte(`{"cursor":"done"}`)
				task.PrivateData.ResultDiscarded = discard
				task.PrivateData.CompletionUsage = &TaskCompletionUsage{Facts: map[string]any{"calls": "1"}}
				const insertCallback = "immediate_retention_insert"
				insertions := 0
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register(insertCallback, func(tx *gorm.DB) {
					if tx.Statement.Table != "tasks" {
						return
					}
					insertions++
					stored, ok := tx.Statement.Dest.(*Task)
					require.True(t, ok)
					if discard {
						assert.Empty(t, stored.Data, "discarded body must never enter INSERT or a database journal")
						assert.Empty(t, stored.PrivateData.PluginState)
						assert.Empty(t, stored.PrivateData.ResultURL)
					} else {
						assert.NotEmpty(t, stored.Data)
						assert.NotEmpty(t, stored.PrivateData.PluginState)
						assert.NotEmpty(t, stored.PrivateData.ResultURL)
					}
					require.NotNil(t, stored.PrivateData.CompletionUsage)
				}))
				t.Cleanup(func() { assert.NoError(t, db.Callback().Create().Remove(insertCallback)) })
				const callback = "immediate_terminal_failure"
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "tasks" {
						tx.AddError(assert.AnError)
					}
				}))
				committed, err := PersistImmediateTask(context.Background(), &task, TaskStatusNotStart, 30, consumption, consumption)
				require.ErrorIs(t, err, assert.AnError)
				assert.False(t, committed)
				assert.Zero(t, task.ID)
				require.NoError(t, db.Callback().Update().Remove(callback))
				var count int64
				require.NoError(t, db.Model(&Task{}).Where("task_id = ?", id).Count(&count).Error)
				assert.Zero(t, count, "the failed terminal write must roll back creation")
				var recovery BillingRecovery
				require.NoError(t, db.First(&recovery, "request_id = ?", id).Error)
				assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
				assert.NotEqual(t, BillingRecoveryStateTaskPending, recovery.ConsumptionState)
				_, estimate, err := FrozenTaskBillingEstimate(id, consumption.AttemptID)
				require.NoError(t, err, "rollback preserves independently committed send evidence")
				assert.EqualValues(t, 30, estimate.FinalQuota)
				committed, err = PersistImmediateTask(context.Background(), &task, TaskStatusNotStart, 30, consumption, consumption)
				require.NoError(t, err)
				require.True(t, committed)
				assert.Equal(t, 2, insertions)
				require.NoError(t, db.First(&recovery, "request_id = ?", id).Error)
				assert.Equal(t, BillingRecoveryStatusSettlementPending, recovery.Status)
				assert.ErrorIs(t, MarkBillingRecoveryRefundPending(id, nil), ErrBillingRecoveryStateConflict)
				var stored Task
				require.NoError(t, db.First(&stored, task.ID).Error)
				assert.EqualValues(t, TaskStatusSuccess, stored.Status)
				require.NotNil(t, stored.PrivateData.CompletionUsage)
				assert.Equal(t, "1", stored.PrivateData.CompletionUsage.Facts["calls"])
				if discard {
					assert.Empty(t, stored.Data)
					assert.Empty(t, stored.PrivateData.PluginState)
					assert.Empty(t, stored.PrivateData.ResultURL)
				} else {
					assert.JSONEq(t, string(task.Data), string(stored.Data))
					assert.Equal(t, task.PrivateData.ResultURL, stored.PrivateData.ResultURL)
				}
				assert.NotEmpty(t, task.Data, "in-memory payload remains available for first delivery")
				assert.NotEmpty(t, task.PrivateData.ResultURL)
				assert.NotEmpty(t, task.PrivateData.PluginState)
				for range 2 {
					completeTaskRecovery(t, id, common.Quota(30), false)
				}
				won, err := PersistTaskCompletion(context.Background(), &task, TaskStatusNotStart, 30, consumption)
				require.NoError(t, err)
				assert.False(t, won)
				assertTaskAccountProjection(t, db, user.Id, token.Id, 70, 0, 30)
				require.NoError(t, db.Model(&TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&count).Error)
				assert.EqualValues(t, 1, count)
				require.NoError(t, db.Model(&BillingEvent{}).Where("request_id = ? AND operation = ?", id, BillingRecoveryOperationSettle).Count(&count).Error)
				assert.EqualValues(t, 1, count)
			})
		}
	})
}
