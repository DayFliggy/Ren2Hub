package router

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayPluginAsyncImagesRetainCompletedResults(t *testing.T) {
	const imageModel = "wan2.5-t2i-preview"
	const imageURL = "https://example.invalid/persisted-image.png"
	var submissions, polls atomic.Int32
	_, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			submissions.Add(1)
			assert.Equal(t, "/api/v1/services/aigc/text2image/image-synthesis", r.URL.Path)
			var request map[string]any
			require.NoError(t, common.DecodeJson(r.Body, &request))
			assert.Equal(t, imageModel, request["model"])
			_, _ = io.WriteString(w, `{"output":{"task_id":"async-image-provider","task_status":"PENDING"}}`)
			return
		}
		polls.Add(1)
		assert.Equal(t, "/api/v1/tasks/async-image-provider", r.URL.Path)
		var accepted model.Task
		require.NoError(t, model.DB.First(&accepted).Error)
		assert.Equal(t, model.TaskStatusNotStart, accepted.Status)
		assert.False(t, accepted.PrivateData.ResultDiscarded, "Images must retain an asynchronously accepted task")
		assert.Contains(t, string(accepted.Data), "PENDING")
		var recovery model.BillingRecovery
		require.NoError(t, model.DB.First(&recovery, "request_id = ?", accepted.PrivateData.SubmissionRequestID).Error)
		assert.Equal(t, model.BillingRecoveryStateTaskPending, recovery.ConsumptionState)
		_, _ = fmt.Fprintf(w, `{"output":{"task_id":"async-image-provider","task_status":"SUCCEEDED","results":[{"url":%q}]},"usage":{"image_count":1}}`, imageURL)
	})
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.TaskResultEvent{}, &model.PerfMetric{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}))
	require.NoError(t, controller.InitTaskPlugins())
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type, channel.Models = constant.ChannelTypeAli, imageModel
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	require.NoError(t, setGatewayModelPrice(imageModel, model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("image_count") * 0.01`}))
	require.NoError(t, service.InitRouteCapabilityIndex(t.Context()))
	previousFactory := service.GetTaskAdaptorFunc
	service.GetTaskAdaptorFunc = func(platform constant.TaskPlatform) service.TaskPollingAdaptor {
		return relay.GetTaskAdaptor(platform)
	}
	t.Cleanup(func() { service.GetTaskAdaptorFunc = previousFactory })
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetTaskPluginProtocolRouter(engine)
	SetTaskRouter(engine)
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)
	status, body := postGatewayTask(t, gateway.URL, key, "/v1/images/generations", fmt.Sprintf(`{"model":%q,"prompt":"async image retention","n":1,"response_format":"url"}`, imageModel))
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, imageURL)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
	assert.False(t, task.PrivateData.ResultDiscarded)
	assert.Contains(t, string(task.Data), imageURL)
	assert.Equal(t, imageURL, task.PrivateData.ResultURL)
	require.NotNil(t, task.PrivateData.CompletionUsage)
	assert.Equal(t, "1", fmt.Sprint(task.PrivateData.CompletionUsage.Facts["image_count"]))
	for _, test := range []struct{ path, expected string }{
		{"/v1/tasks/" + task.TaskID, `"status":"SUCCESS"`},
		{"/v1/tasks/" + task.TaskID + "/artifacts", `"key":"image-1"`},
	} {
		request, err := http.NewRequest(http.MethodGet, gateway.URL+test.path, nil)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		payload, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusOK, response.StatusCode, string(payload))
		assert.Contains(t, string(payload), test.expected)
	}
	assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusSettled, 5000)
	assert.EqualValues(t, 1, submissions.Load(), "polling and retrieval cannot submit generation again")
	assert.EqualValues(t, 1, polls.Load())
	var results int64
	require.NoError(t, model.DB.Model(&model.TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&results).Error)
	assert.EqualValues(t, 1, results)
}
