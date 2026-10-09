package router

import (
	"context"
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
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Seedance completion results report tokens and resolution but never whether
// the request carried reference video or asked for silent output. A price
// conditioned on those request facts must apply them at settlement instead of
// silently taking its default branch.
func TestGatewayDoubaoVideoSettlesConditionsOnSubmitFacts(t *testing.T) {
	for _, test := range []struct {
		name, model, expression, metadata string
		estimate, charge                  common.Quota
	}{
		// 108000 estimated tokens × 0.6 reserve 0.648 USD; 100000 actual tokens settle 0.6 USD.
		{"reference video discount", "doubao-seedance-2-0-260128",
			`u("tokens") * 0.00001 * (u("video_input") == "video" ? 0.6 : 1)`,
			`{"resolution":"720p","content":[{"type":"video_url","video_url":{"url":"https://cdn.example/in.mp4"}}]}`, 324000, 300000},
		// 108000 estimated tokens × 0.5 reserve 0.54 USD; 100000 actual tokens settle 0.5 USD.
		{"silent output discount", "doubao-seedance-1-5-pro-251215",
			`u("tokens") * 0.00001 * (u("generate_audio") == false ? 0.5 : 1)`,
			`{"resolution":"720p","generate_audio":false}`, 270000, 250000},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v3/contents/generations/tasks", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprint(w, `{"id":"cgt-fixture"}`)
			})
			for _, id := range []int{1, 4} {
				channel, err := model.GetChannelById(id, true)
				require.NoError(t, err)
				channel.Type, channel.Models = constant.ChannelTypeDoubaoVideo, test.model
				require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
			}
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			require.NoError(t, setGatewayModelPrice(test.model, model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": test.expression}))

			code, body := postGatewayTask(t, base, key, "/v1/videos",
				fmt.Sprintf(`{"model":%q,"prompt":"a fox","seconds":"5","metadata":%s}`, test.model, test.metadata))
			require.Equal(t, http.StatusOK, code, body)
			var task model.Task
			require.NoError(t, model.DB.Order("id desc").First(&task).Error)
			require.Equal(t, test.estimate, task.Quota)
			var user model.User
			require.NoError(t, model.DB.First(&user, task.UserId).Error)
			usedBefore := user.UsedQuota

			plugin, ok := jsplugin.DefaultRegistry.Get("doubao")
			require.True(t, ok)
			result, err := taskplugin.New(plugin).ParseTaskResultForTask(&task, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)},
				[]byte(`{"id":"cgt-fixture","status":"succeeded","usage":{"completion_tokens":100000},"content":{"video_url":"https://cdn.example/out.mp4","resolution":"720p"}}`))
			require.NoError(t, err)
			require.Empty(t, result.UsageError)
			require.NoError(t, service.CompleteImmediateTask(context.Background(), &task, result))

			require.NoError(t, model.DB.First(&user, task.UserId).Error)
			assert.Equal(t, test.charge, user.UsedQuota-usedBefore)
		})
	}
}

// Z-Image results report the image count but not whether prompt rewriting was
// enabled. The synchronous result settles inside the submit request, and the
// requested prompt_extend must still price it.
func TestGatewayAliZImageImmediateResultSettlesPromptExtend(t *testing.T) {
	const imageModel = "z-image-turbo"
	_, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"request_id":"ali-fixture","output":{"choices":[{"message":{"content":[{"image":"https://cdn.example/fox.png"}]}}],"finished":true},"usage":{"image_count":1}}`)
	})
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.TaskResultEvent{}, &model.PerfMetric{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}, &model.GatewayRequestDiagnostic{}))
	require.NoError(t, controller.InitTaskPlugins())
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type, channel.Models = constant.ChannelTypeAli, imageModel
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	// Prompt rewriting doubles the 0.04 USD image price.
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
	require.Equal(t, http.StatusOK, code, body)
	assert.Contains(t, body, "https://cdn.example/fox.png")

	var task model.Task
	require.NoError(t, model.DB.Order("id desc").First(&task).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
	assert.EqualValues(t, 40000, task.Quota)
	usage := task.PrivateData.BillingContext.Usage
	require.NotNil(t, usage)
	assert.False(t, billing.IsFrozenEstimateSource(usage.Source), "the completion facts priced the result")
	assert.Equal(t, true, usage.EstimatedFacts["prompt_extend"])
	assert.NotContains(t, usage.ActualFacts, "prompt_extend")
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
}
