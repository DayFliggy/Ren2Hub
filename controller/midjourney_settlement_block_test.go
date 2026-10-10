package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Midjourney success whose attempt evidence no longer verifies keeps its
// provider result and reservation instead of being released as timed out,
// and settles from the held result once the evidence verifies again.
func TestMidjourneySettlementBlockedResultSurvivesTimeout(t *testing.T) {
	db := setupMidjourneyPoll(t)
	// The blocked-settlement metric counts both async job tables.
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `[{"id":"provider-job","status":"SUCCESS","progress":"100%","imageUrl":"https://fixture.invalid/result"}]`)
	}))
	defer upstream.Close()
	task := seedMidjourneyPollTask(t, db, upstream.URL, "original", "provider-job")
	var attempt model.BillingAttempt
	require.NoError(t, db.First(&attempt, "id = ?", task.PrivateData.BillingContext.AttemptID).Error)
	original := string(attempt.EstimateUsage)
	var estimate billing.Usage
	require.NoError(t, common.UnmarshalJsonStr(original, &estimate))
	estimate.Quantities["calls"] = "2"
	tampered, err := common.Marshal(estimate)
	require.NoError(t, err)
	require.NoError(t, db.Model(&attempt).Update("estimate_usage", string(tampered)).Error)
	previous := constant.TaskTimeoutMinutes
	t.Cleanup(func() { constant.TaskTimeoutMinutes = previous })

	constant.TaskTimeoutMinutes = 0
	_, err = runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, err)
	// The job was submitted two hours ago; a one-minute timeout now applies.
	constant.TaskTimeoutMinutes = 1
	_, err = runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, err)

	require.NoError(t, db.First(&task, task.Id).Error)
	assert.Equal(t, "SUBMITTED", task.Status)
	assert.Equal(t, model.TaskSettlementBlockedEvidence, task.SettlementBlock)
	assert.Contains(t, string(task.PrivateData.BlockedResult), "https://fixture.invalid/result")
	assert.EqualValues(t, 20, task.Quota)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 80, user.Quota, "the reservation stays held")
	metrics, err := service.ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, metrics.Recovery.SettlementBlocked)
	assert.EqualValues(t, 1, sends.Load(), "a held result is not polled again")

	require.NoError(t, db.Model(&attempt).Update("estimate_usage", original).Error)
	_, err = runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, db.First(&task, task.Id).Error)
	assert.Equal(t, "SUCCESS", task.Status)
	assert.Empty(t, task.SettlementBlock)
	assert.Equal(t, "https://fixture.invalid/result", task.ImageUrl)
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, 20, user.UsedQuota)
	metrics, err = service.ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	assert.Zero(t, metrics.Recovery.SettlementBlocked)
}
