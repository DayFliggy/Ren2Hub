package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tamperAttemptEstimate alters the task attempt's persisted estimate so its
// evidence no longer replays, as a Go toolchain change does to recorded time
// values. The returned function restores the original row.
func tamperAttemptEstimate(t *testing.T, task *model.Task) func() {
	t.Helper()
	var attempt model.BillingAttempt
	require.NoError(t, model.DB.First(&attempt, "id = ?", task.PrivateData.BillingContext.AttemptID).Error)
	original := string(attempt.EstimateUsage)
	var estimate billing.Usage
	require.NoError(t, common.UnmarshalJsonStr(original, &estimate))
	estimate.EstimatedFacts["seconds"] = "1"
	tampered, err := common.Marshal(estimate)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&attempt).Update("estimate_usage", string(tampered)).Error)
	return func() {
		require.NoError(t, model.DB.Model(&attempt).Update("estimate_usage", original).Error)
	}
}

// withTaskTimeoutMinutes also sets the polling batch size, which tests do not
// load from the environment.
func withTaskTimeoutMinutes(t *testing.T, minutes int) {
	t.Helper()
	previousTimeout, previousLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
	constant.TaskTimeoutMinutes, constant.TaskQueryLimit = minutes, 100
	t.Cleanup(func() { constant.TaskTimeoutMinutes, constant.TaskQueryLimit = previousTimeout, previousLimit })
}

func readSettlementBlocked(t *testing.T) int64 {
	t.Helper()
	metrics, err := ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	return metrics.Recovery.SettlementBlocked
}

// A provider success whose attempt evidence no longer verifies must not end as
// a free "no result" timeout: the result is held, the reservation stays held,
// polling and the sweep leave it alone, and the metric reports it. Once the
// evidence verifies again the retry pass settles the held result.
func TestSettlementBlockedTaskHoldsResultAcrossPollingAndSweep(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	task := acceptedUsageTask(t, quote, submitted, 1500000)
	restore := tamperAttemptEstimate(t, task)
	withTaskTimeoutMinutes(t, 1)
	held := 100000000 - 1500000

	outcome := &taskPollOutcome{data: []byte(`{"status":"succeeded"}`), info: &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess, Url: "https://fixture.invalid/out.mp4", UsageFacts: map[string]any{"audio": false}}}
	require.NoError(t, applyTaskPollResult(context.Background(), nil, task, http.StatusOK, outcome))

	assertBlocked := func(stage string) {
		t.Helper()
		var persisted model.Task
		require.NoError(t, model.DB.First(&persisted, task.ID).Error)
		assert.EqualValues(t, model.TaskStatusInProgress, persisted.Status, stage)
		assert.Equal(t, model.TaskSettlementBlockedEvidence, persisted.SettlementBlock, stage)
		assert.Empty(t, persisted.PrivateData.ResultURL, "%s: the result is not delivered before settlement", stage)
		var result model.TaskBlockedResult
		require.NoError(t, common.Unmarshal(persisted.PrivateData.BlockedResult, &result), stage)
		assert.EqualValues(t, model.TaskStatusSuccess, result.Status, stage)
		assert.Equal(t, "https://fixture.invalid/out.mp4", result.ResultURL, stage)
		var recovery model.BillingRecovery
		require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
		assert.Equal(t, model.BillingRecoveryStatusActive, recovery.Status, stage)
		assert.Equal(t, model.BillingRecoveryStateTaskPending, recovery.ConsumptionState, stage)
		assert.Equal(t, held, getUserQuota(t, task.UserId), "%s: the reservation stays held", stage)
		assert.EqualValues(t, 1, readSettlementBlocked(t), stage)
	}
	assertBlocked("after polling")
	assert.Empty(t, model.GetAllUnFinishSyncTasks(100), "a held result is never polled again")

	sweepTimedOutTasks(context.Background())
	assertBlocked("after the timeout sweep")
	retrySettlementBlockedTasks(context.Background())
	assertBlocked("after a retry with the same evidence")

	restore()
	retrySettlementBlockedTasks(context.Background())
	var settled model.Task
	require.NoError(t, model.DB.First(&settled, task.ID).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, settled.Status)
	assert.Empty(t, settled.SettlementBlock)
	assert.Empty(t, settled.PrivateData.BlockedResult)
	assert.Equal(t, "https://fixture.invalid/out.mp4", settled.PrivateData.ResultURL)
	assert.EqualValues(t, 750000, settled.Quota)
	assert.Equal(t, 100000000-750000, getUserQuota(t, task.UserId))
	assert.Zero(t, readSettlementBlocked(t))
}

// The sweep filters held results in SQL, so they cannot fill its batch, and a
// task that genuinely never returned a result is still released.
func TestTimedOutTaskWithoutResultStillReleasesBesideBlockedTask(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	blocked := acceptedUsageTask(t, quote, submitted, 1500000)
	tamperAttemptEstimate(t, blocked)
	require.NoError(t, CompleteImmediateTask(context.Background(), blocked, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: map[string]any{"audio": false}}))

	plain := makeTask(blocked.UserId, blocked.ChannelId, 1000, 0, BillingSourceWallet)
	plain.TaskID, plain.SubmitTime = "task_plain_timeout", 1
	acceptTestTask(t, plain)
	withTaskTimeoutMinutes(t, 1)

	oldest := model.GetTimedOutUnfinishedTasks(time.Now().Unix(), 1)
	require.Len(t, oldest, 1)
	assert.Equal(t, plain.ID, oldest[0].ID, "the older held task does not take the batch slot")

	sweepTimedOutTasks(context.Background())
	var released, held model.Task
	require.NoError(t, model.DB.First(&released, plain.ID).Error)
	assert.EqualValues(t, model.TaskStatusFailure, released.Status)
	assert.Equal(t, constant.TaskFailReasonTimedOut, released.FailReason)
	assert.Zero(t, released.Quota)
	require.NoError(t, model.DB.First(&held, blocked.ID).Error)
	assert.Equal(t, model.TaskSettlementBlockedEvidence, held.SettlementBlock)
	assert.NotEqual(t, model.TaskStatusFailure, string(held.Status))
	assert.Equal(t, 100000000-1500000, getUserQuota(t, blocked.UserId), "only the held task's reservation remains")
}
