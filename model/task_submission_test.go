package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func TestAcceptedTaskAndAccountingCommitTogether(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		user := User{Username: "task-txn", Quota: 100}
		require.NoError(t, db.Create(&user).Error)
		token := Token{UserId: user.Id, Key: "isolated-task-txn", RemainQuota: 100}
		require.NoError(t, db.Create(&token).Error)
		channel := Channel{Name: "task-txn"}
		require.NoError(t, db.Create(&channel).Error)
		_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: "task-txn", UserID: user.Id, TokenID: token.Id, Source: "wallet", TokenRequired: true})
		require.NoError(t, err)
		require.NoError(t, Reserve("task-txn", 40))
		task := &Task{TaskID: "task_public", UserId: user.Id, ChannelId: channel.Id, Quota: 30, Status: TaskStatusNotStart, PrivateData: TaskPrivateData{UpstreamTaskID: "provider-private", TokenId: token.Id, SubmissionRequestID: "task-txn"}}
		consumption := BillingConsumption{Log: Log{UserId: user.Id, TokenId: token.Id, ChannelId: channel.Id, Quota: 30, ModelName: "task-test", Type: LogTypeConsume}, CountUsage: true, LogEnabled: true}
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("task_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "tasks" {
				tx.AddError(assert.AnError)
			}
		}))
		require.ErrorIs(t, PersistAcceptedTask(task, "task-txn", consumption), assert.AnError)
		var recovery BillingRecovery
		require.NoError(t, db.First(&recovery, "request_id = ?", "task-txn").Error)
		assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
		assert.Empty(t, recovery.ConsumptionSnapshot)
		require.NoError(t, db.Callback().Create().Remove("task_failure"))
		for range 2 {
			require.NoError(t, PersistAcceptedTask(task, "task-txn", consumption))
		}
		var count int64
		require.NoError(t, db.Model(&Task{}).Count(&count).Error)
		assert.EqualValues(t, 1, count)
		require.NoError(t, db.First(&recovery, "request_id = ?", "task-txn").Error)
		assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
		assert.Equal(t, BillingRecoveryStateTaskPending, recovery.ConsumptionState)
		assert.ErrorIs(t, MarkBillingRecoveryRefundPending("task-txn", nil), ErrBillingRecoveryStateConflict)
		// Recovery fallbacks leave an accepted task's accounting to its polling.
		assert.ErrorIs(t, PrepareFrozenEstimateConsumption("task-txn", "", Log{}, true), ErrBillingTaskPending)
		ready, err := TaskSubmissionReady(context.Background(), task)
		require.NoError(t, err)
		assert.True(t, ready)
		assertTaskAccountProjection(t, db, user.Id, token.Id, 60, 40, 0)
		require.NoError(t, db.First(&user, user.Id).Error)
		assert.Zero(t, user.RequestCount)
		require.NoError(t, db.Model(&Log{}).Count(&count).Error)
		assert.Zero(t, count)
		require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
		assert.EqualValues(t, 1, count)
		changed := consumption
		changed.Log.Quota = common.Quota(31)
		assert.ErrorIs(t, prepareTaskAcceptedBillingTx(db, "task-txn", changed), ErrBillingEventConflict)
	})
}

// The batched check keeps TaskSubmissionReady's rules: only an accepted
// submission whose recovery belongs to the same owner is polled.
func TestTaskSubmissionsReadyMatchesPerTaskRules(t *testing.T) {
	withTaskBillingDatabases(t, func(t *testing.T, db *gorm.DB) {
		for _, row := range []BillingRecovery{
			{RequestID: "ready", UserID: 1, TokenID: 2, Status: BillingRecoveryStatusActive, ConsumptionState: BillingRecoveryStateTaskPending},
			{RequestID: "other-owner", UserID: 9, TokenID: 2, Status: BillingRecoveryStatusActive, ConsumptionState: BillingRecoveryStateTaskPending},
			{RequestID: "settled", UserID: 1, TokenID: 2, Status: BillingRecoveryStatusSettled, ConsumptionState: BillingRecoveryStateTaskPending},
		} {
			require.NoError(t, db.Create(&row).Error)
		}
		task := func(id, requestID string) *Task {
			return &Task{TaskID: id, UserId: 1, PrivateData: TaskPrivateData{TokenId: 2, SubmissionRequestID: requestID}}
		}
		tasks := []*Task{task("a", "ready"), task("b", "other-owner"), task("c", "settled"), task("d", "missing"), task("e", "")}

		ready, err := TaskSubmissionsReady(context.Background(), tasks)
		require.Len(t, ready, 1)
		assert.Equal(t, "a", ready[0].TaskID)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrBillingRecoveryStateConflict)
		for _, id := range []string{"task b", "task d", "task e"} {
			assert.Contains(t, err.Error(), id)
		}
		assert.NotContains(t, err.Error(), "task c")
		for _, item := range tasks[:3] {
			single, _ := TaskSubmissionReady(context.Background(), item)
			assert.Equal(t, item.TaskID == "a", single)
		}
	})
}
