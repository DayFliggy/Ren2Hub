package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An immediate result whose attempt estimate changes while the provider runs
// cannot be settled. The client gets an explicit error instead of the image,
// and the job is saved holding the result and its reservation for a later
// settlement, rather than being dropped with the reservation stuck.
func TestGatewayImmediateResultWithUnverifiableEstimateIsHeld(t *testing.T) {
	const imageModel = "z-image-turbo"
	_, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		var attempts []model.BillingAttempt
		assert.NoError(t, model.DB.Where("estimate_usage <> ''").Find(&attempts).Error)
		for _, attempt := range attempts {
			var usage billing.Usage
			assert.NoError(t, common.UnmarshalJsonStr(string(attempt.EstimateUsage), &usage))
			usage.InputTokens++
			altered, err := common.Marshal(usage)
			assert.NoError(t, err)
			assert.NoError(t, model.DB.Model(&attempt).Update("estimate_usage", string(altered)).Error)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"request_id":"ali-fixture","output":{"choices":[{"message":{"content":[{"image":"https://cdn.example/fox.png"}]}}],"finished":true},"usage":{"image_count":1}}`)
	})
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.Midjourney{}, &model.TaskResultEvent{}, &model.PerfMetric{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}, &model.GatewayRequestDiagnostic{}))
	require.NoError(t, controller.InitTaskPlugins())
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type, channel.Models = constant.ChannelTypeAli, imageModel
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	require.NoError(t, setGatewayModelPrice(imageModel, model.PricingValues{"billing_setting.billing_mode": "tiered_expr",
		"billing_setting.billing_expr": `u("image_count") * 0.04 * (u("prompt_extend") == true ? 2 : 1)`}))
	require.NoError(t, service.InitRouteCapabilityIndex(t.Context()))
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetTaskPluginProtocolRouter(engine)
	SetTaskRouter(engine)
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)

	code, body := postGatewayTask(t, gateway.URL, key, "/v1/images/generations",
		fmt.Sprintf(`{"model":%q,"prompt":"a fox","prompt_extend":true,"response_format":"url"}`, imageModel))
	require.Equal(t, http.StatusServiceUnavailable, code, body)
	assert.Contains(t, body, "task_settlement_blocked")
	assert.NotContains(t, body, "fox.png", "an unsettled result is not delivered")

	var task model.Task
	require.NoError(t, model.DB.Order("id desc").First(&task).Error)
	assert.NotEqual(t, model.TaskStatusSuccess, string(task.Status))
	assert.Equal(t, model.TaskSettlementBlockedEvidence, task.SettlementBlock)
	assert.False(t, task.PrivateData.ResultDiscarded, "the undelivered result is kept for retrieval after settlement")
	assert.Empty(t, task.PrivateData.ResultURL)
	assert.Contains(t, string(task.PrivateData.BlockedResult), "fox.png")
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
	assert.Equal(t, model.BillingRecoveryStatusActive, recovery.Status)
	assert.Equal(t, model.BillingRecoveryStateTaskPending, recovery.ConsumptionState)
	assert.Positive(t, recovery.FundingAmount, "the reservation stays held")
	metrics, err := service.ReadGatewayMetrics(t.Context())
	require.NoError(t, err)
	assert.EqualValues(t, 1, metrics.Recovery.SettlementBlocked)
}
