package controller

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// An immediate task success never reaches the polling terminal transition, so
// the submission itself must contribute exactly one model performance sample.
func TestExecuteTaskSubmissionSamplesSettledImmediateSuccessOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		immediate *relaycommon.TaskInfo
		settleErr error
		want      int64
	}{
		{"settled immediate success", &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%"}, nil, 1},
		{"immediate failure", &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "rejected"}, nil, 0},
		{"asynchronous submission", nil, nil, 0},
		{"settlement failure", &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, Progress: "100%"}, errors.New("settlement failed"), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make([]string, 0, 3)
			database := setupTaskSubmissionDatabase(t, true, &events)
			require.NoError(t, database.AutoMigrate(&model.PerfMetric{}))
			previousLogConsumeEnabled := common.LogConsumeEnabled
			common.LogConsumeEnabled = false
			t.Cleanup(func() { common.LogConsumeEnabled = previousLogConsumeEnabled })

			info := taskSubmissionRelayInfo(&taskSubmissionTestBilling{events: &events, settleErr: tc.settleErr})
			info.OriginModelName = "image-model"
			info.StartTime = time.Now()
			before := taskPerfRequests(t, info.OriginModelName)

			_, _ = executeTaskSubmissionWith(taskSubmissionTestContext(), info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				return &relay.TaskSubmitResult{
					UpstreamTaskID: "upstream_private",
					Platform:       constant.TaskPlatform("plugin"),
					Immediate:      tc.immediate,
				}, nil
			})

			require.Equal(t, tc.want, taskPerfRequests(t, info.OriginModelName)-before)
		})
	}
}

func taskPerfRequests(t *testing.T, modelName string) int64 {
	t.Helper()
	result, err := perfmetrics.QuerySummaryAll(1)
	require.NoError(t, err)
	for _, summary := range result.Models {
		if summary.ModelName == modelName {
			return summary.RequestCount
		}
	}
	return 0
}
