package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An accepted async task refreshes its recovery row whenever polling touches
// it, so it can stay younger than the stale cutoff for its whole life. The
// open count must report it anyway: replaying its evidence on another Go
// toolchain fails, so an upgrade gate needs every in-flight record.
func TestGatewayMetricsOpenCountsFreshAcceptedTask(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	task := acceptedUsageTask(t, quote, submitted, 1500000)
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Where("request_id = ?", task.PrivateData.SubmissionRequestID).
		Update("updated_at", time.Now().Unix()).Error)

	metrics, err := ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	assert.Zero(t, metrics.Recovery.Pending, "a fresh accepted task is not stale backlog")
	assert.EqualValues(t, 1, metrics.Recovery.Open)

	require.NoError(t, CompleteImmediateTask(context.Background(), task, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: map[string]any{"audio": false}}))
	metrics, err = ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	assert.Zero(t, metrics.Recovery.Open, "a settled and delivered task is no longer open")
}
