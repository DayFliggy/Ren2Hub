package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const gatewaySeedreamModel = "doubao-seedream-5-0-lite-260128"

func setupGatewayDoubaoImage(t *testing.T, handler http.HandlerFunc) (string, string) {
	t.Helper()
	_, key, _ := setupGatewaySDKWithUpstream(t, handler)
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.TaskResultEvent{}, &model.PerfMetric{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}, &model.GatewayRequestDiagnostic{}))
	require.NoError(t, controller.InitTaskPlugins())
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type, channel.Models = constant.ChannelTypeDoubaoVideo, gatewaySeedreamModel
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	require.NoError(t, setGatewayModelPrice(gatewaySeedreamModel, model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("images_above_1_5k") * 0.01`}))
	require.NoError(t, service.InitRouteCapabilityIndex(t.Context()))
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetTaskPluginProtocolRouter(engine)
	SetTaskRouter(engine)
	gateway := httptest.NewServer(engine)
	t.Cleanup(gateway.Close)
	return gateway.URL, key
}

func assertGatewayDoubaoSettledOnce(t *testing.T, charge common.Quota) model.Task {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
	assert.True(t, task.PrivateData.ResultDiscarded)
	assert.Empty(t, task.Data, "synchronous result payload must not remain in storage")
	assert.Empty(t, task.PrivateData.ResultURL)
	assert.Empty(t, task.PrivateData.PluginState)
	require.NotNil(t, task.PrivateData.BillingContext)
	assert.NotEmpty(t, task.PrivateData.SubmissionRequestID)
	assert.NotEmpty(t, task.PrivateData.PluginAttemptID)
	assert.NotEmpty(t, task.PrivateData.PluginRef.SourceHash)
	assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusSettled, charge)
	// A duplicate terminal result is a losing CAS and cannot change the winner.
	require.NoError(t, service.CompleteImmediateTask(t.Context(), &task, &relaycommon.TaskInfo{Status: "SUCCESS", UsageFacts: map[string]any{"images_above_1_5k": "2", "images_up_to_1_5k": "0"}}))
	var count int64
	require.NoError(t, model.DB.Model(&model.TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", task.PrivateData.SubmissionRequestID, "settle").Count(&count).Error)
	assert.EqualValues(t, 1, count)
	return task
}

func TestGatewayDoubaoImagesSettleAndDiscardWithoutResubmission(t *testing.T) {
	for _, test := range []struct {
		name, responseFormat, generated, outputURL string
		status                                     int
		charge                                     common.Quota
	}{
		{"delivered count without size", "url", "2", "https://cdn.example/one.png", http.StatusOK, 10000},
		{"invalid count uses frozen estimate", "url", "1.5", "https://cdn.example/one.png", http.StatusOK, 20000},
		{"download failure still settles accepted generation", "b64_json", "2", "http://127.0.0.1:1/unavailable.png", http.StatusBadGateway, 10000},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			base, key := setupGatewayDoubaoImage(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "/api/v3/images/generations", r.URL.Path)
				var body map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &body))
				assert.Equal(t, gatewaySeedreamModel, body["model"])
				assert.NotContains(t, body, "response_format", "Base64 remains a host output conversion")
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"data":[{"url":%q},{"url":"https://cdn.example/two.png"},{"error":{"code":"blocked"}}],"usage":{"generated_images":%s}}`, test.outputURL, test.generated)
			})
			body := fmt.Sprintf(`{"model":%q,"prompt":"isolated test","sequential_image_generation":"auto","sequential_image_generation_options":{"max_images":4},"response_format":%q}`, gatewaySeedreamModel, test.responseFormat)
			status, response := postGatewayTask(t, base, key, "/v1/images/generations", body)
			require.Equal(t, test.status, status, response)
			if test.status == http.StatusOK {
				assert.Contains(t, response, test.outputURL)
				assert.NotContains(t, response, "blocked")
			} else {
				assert.Contains(t, response, "image_download_failed")
			}
			task := assertGatewayDoubaoSettledOnce(t, test.charge)
			assert.EqualValues(t, 1, sends.Load())
			for _, path := range []string{"/v1/tasks/" + task.TaskID, "/v1/tasks/" + task.TaskID + "/artifacts", "/v1/responses/resp_" + strings.TrimPrefix(task.TaskID, "task_")} {
				request, err := http.NewRequest(http.MethodGet, base+path, nil)
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+key)
				response, err := http.DefaultClient.Do(request)
				require.NoError(t, err)
				_, err = io.Copy(io.Discard, response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				assert.Equal(t, http.StatusNotFound, response.StatusCode, path)
				assert.Contains(t, response.Header.Get("Content-Type"), "application/json", "the production handler must reject the existing discarded task")
			}
		})
	}
}

func TestGatewayDoubaoRejectedProtocolRequestsNeverReserveOrSend(t *testing.T) {
	var sends atomic.Int32
	base, key := setupGatewayDoubaoImage(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		http.Error(w, "unexpected send", http.StatusInternalServerError)
	})
	for _, test := range []struct{ path, input string }{
		{"/v1/images/generations", `"prompt":"cat","n":2`},
		{"/v1/images/generations", `"prompt":"cat","stream":true`},
		{"/v1/images/edits", `"prompt":"cat"`},
		{"/v1/images/edits", `"prompt":"cat","image":"https://cdn.example/a.png","mask":"https://cdn.example/mask.png"`},
	} {
		code, body := postGatewayTask(t, base, key, test.path, fmt.Sprintf(`{"model":%q,%s}`, gatewaySeedreamModel, test.input))
		assert.Equal(t, http.StatusBadRequest, code, body)
	}
	assert.Zero(t, sends.Load())
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestGatewayDoubaoClientCancellationAndPluginUpdateKeepFrozenAttempt(t *testing.T) {
	var sends atomic.Int32
	accepted, release := make(chan struct{}), make(chan struct{})
	base, key := setupGatewayDoubaoImage(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		close(accepted)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/1.png"},{"url":"https://cdn.example/2.png"}],"usage":{"generated_images":2}}`)
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/images/generations", strings.NewReader(fmt.Sprintf(`{"model":%q,"prompt":"cancelled test"}`, gatewaySeedreamModel)))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	clientResult := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		clientResult <- err
	}()
	select {
	case <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was not sent")
	}
	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, restoreErr := jsplugin.DefaultRegistry.RegisterFactory(source, jsplugin.Options{Key: "doubao"})
		require.NoError(t, restoreErr)
	})
	factory, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	_, err = jsplugin.DefaultRegistry.RegisterFactory(strings.Replace(source, `version: "`+factory.Meta.Version+`"`, `version: "9.9.2"`, 1), jsplugin.Options{Key: "doubao"})
	require.NoError(t, err)
	require.NoError(t, setGatewayModelPrice(gatewaySeedreamModel, model.PricingValues{"ModelPrice": "9"}))
	cancel()
	close(release)
	require.ErrorIs(t, <-clientResult, context.Canceled)
	require.Eventually(t, func() bool {
		var task model.Task
		if model.DB.First(&task).Error != nil || task.Status != model.TaskStatusSuccess {
			return false
		}
		var recovery model.BillingRecovery
		return model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error == nil && recovery.Status == model.BillingRecoveryStatusSettled
	}, 5*time.Second, 10*time.Millisecond)
	task := assertGatewayDoubaoSettledOnce(t, 10000)
	assert.Equal(t, factory.Meta.Version, task.PrivateData.PluginRef.Version)
	assert.Equal(t, `u("images_above_1_5k") * 0.01`, task.PrivateData.BillingContext.Quote.PriceVersion.Expression)
	assert.EqualValues(t, 1, sends.Load())
}

func TestGatewayDoubaoImmediatePersistenceFailureUsesFrozenEstimate(t *testing.T) {
	var sends atomic.Int32
	base, key := setupGatewayDoubaoImage(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/one.png"},{"url":"https://cdn.example/two.png"}],"usage":{"generated_images":2}}`)
	})
	const callback = "fail_immediate_terminal_write"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove(callback) })
	status, body := postGatewayTask(t, base, key, "/v1/images/generations", fmt.Sprintf(`{"model":%q,"prompt":"atomic persistence","sequential_image_generation":"auto","sequential_image_generation_options":{"max_images":4}}`, gatewaySeedreamModel))
	require.Equal(t, http.StatusServiceUnavailable, status, body)
	require.NoError(t, model.DB.Callback().Update().Remove(callback))
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&count).Error)
	assert.Zero(t, count, "failed transaction must not leave an unfinished task for timeout refunds")
	require.NoError(t, model.DB.Model(&model.TaskResultEvent{}).Count(&count).Error)
	assert.Zero(t, count)
	for range 2 {
		_, err := service.RecoverStaleBillingRecoveries(t.Context(), common.GetTimestamp()+1, 100)
		require.NoError(t, err)
	}
	assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusSettled, 20000)
	assert.EqualValues(t, 1, sends.Load(), "the accepted generation must never be sent again")
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery).Error)
	assert.NotEqual(t, model.BillingRecoveryStateTaskPending, recovery.ConsumptionState)
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", recovery.RequestID, "settle").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

// A committed immediate result is returned even when funds delivery fails;
// billing recovery settles it once, so the client is never charged for a
// result it did not receive.
func TestGatewayDoubaoImmediateDeliveryFailureReturnsResultAndKeepsSettlement(t *testing.T) {
	var sends atomic.Int32
	base, key := setupGatewayDoubaoImage(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/one.png"}],"usage":{"generated_images":1}}`)
	})
	const callback = "fail_immediate_funds_delivery"
	require.NoError(t, model.DB.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "tokens" && sends.Load() > 0 {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { model.DB.Callback().Update().Remove(callback) })
	status, body := postGatewayTask(t, base, key, "/v1/images/generations", fmt.Sprintf(`{"model":%q,"prompt":"pending delivery"}`, gatewaySeedreamModel))
	require.Equal(t, http.StatusOK, status, body)
	assert.Contains(t, body, "https://cdn.example/one.png")
	require.NoError(t, model.DB.Callback().Update().Remove(callback))
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
	require.NotNil(t, task.PrivateData.CompletionUsage)
	assert.Empty(t, task.Data)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettlementPending, recovery.Status)
	for range 2 {
		summary, err := service.RecoverStaleBillingRecoveries(t.Context(), common.GetTimestamp()+1, 100)
		require.NoError(t, err)
		assert.Zero(t, summary.Refunded)
		assert.Zero(t, summary.Failed)
	}
	assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusSettled, 5000)
	assert.EqualValues(t, 1, sends.Load())
}

func TestGatewayDoubaoResponsesRetainImmediateResultsInAllModes(t *testing.T) {
	for _, mode := range []string{`"stream":false`, `"stream":true`, `"background":true`} {
		t.Run(mode, func(t *testing.T) {
			var sends atomic.Int32
			base, key := setupGatewayDoubaoImage(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/one.png"}],"usage":{"generated_images":1}}`)
			})
			status, body := postGatewayTask(t, base, key, "/v1/responses", fmt.Sprintf(`{"model":%q,"input":"retention",%s}`, gatewaySeedreamModel, mode))
			require.Equal(t, http.StatusOK, status, body)
			var task model.Task
			require.NoError(t, model.DB.First(&task).Error)
			assert.False(t, task.PrivateData.ResultDiscarded)
			assert.NotEmpty(t, task.Data)
			if mode != `"background":true` {
				assert.Contains(t, body, "![Image 1]")
				assert.Contains(t, body, "completed")
			}
			request, err := http.NewRequest(http.MethodGet, base+"/v1/responses/resp_"+strings.TrimPrefix(task.TaskID, "task_"), nil)
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			data, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, http.StatusOK, response.StatusCode, string(data))
			assert.Contains(t, string(data), "![Image 1]")
			assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusSettled, 5000)
			assert.EqualValues(t, 1, sends.Load())
		})
	}
}

func TestGatewayNativeRetentionOnlyDiscardsImmediateResults(t *testing.T) {
	for _, immediate := range []bool{true, false} {
		t.Run(fmt.Sprintf("immediate=%t", immediate), func(t *testing.T) {
			var sends atomic.Int32
			_, key := setupGatewayDoubaoImage(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				sends.Add(1)
				_, _ = io.WriteString(w, `{"data":[{"url":"https://cdn.example/one.png"}],"usage":{"generated_images":1}}`)
			})
			source, err := plugins.Source("doubao")
			require.NoError(t, err)
			if !immediate {
				factory, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
				require.NoError(t, err)
				modified := strings.Replace(source, `version: "`+factory.Meta.Version+`"`, `version: "9.9.3"`, 1)
				immediateReturn := `return { taskId: taskId, taskData: body, immediate: { status: "SUCCESS", progress: "100%", url: trimmed(urls[0].url) } };`
				require.Contains(t, modified, immediateReturn)
				modified = strings.Replace(modified, immediateReturn, `return { taskId: taskId, taskData: body };`, 1)
				plugin, err := jsplugin.DefaultRegistry.RegisterFactory(modified, jsplugin.Options{Key: "doubao"})
				require.NoError(t, err)
				t.Cleanup(func() {
					_, restoreErr := jsplugin.DefaultRegistry.RegisterFactory(source, jsplugin.Options{Key: "doubao"})
					require.NoError(t, restoreErr)
				})
				version := model.TaskPlugin{Key: plugin.Meta.Key, Version: plugin.Meta.Version, APIVersion: 1, Source: model.LongText(modified), SourceHash: plugin.SourceHash, Factory: true, Enabled: true}
				require.NoError(t, model.SaveTaskPlugin(&version))
				require.NoError(t, service.InitRouteCapabilityIndex(t.Context()))
			}
			engine := gin.New()
			engine.Use(middleware.RequestId())
			engine.NoRoute(SetPluginRouter(engine))
			gateway := httptest.NewServer(engine)
			t.Cleanup(gateway.Close)
			status, body := postGatewayTask(t, gateway.URL, key, "/doubao/api/v3/images/generations", fmt.Sprintf(`{"model":%q,"prompt":"native retention"}`, gatewaySeedreamModel))
			require.Equal(t, http.StatusOK, status, body)
			assert.Contains(t, body, "https://cdn.example/one.png")
			var task model.Task
			require.NoError(t, model.DB.First(&task).Error)
			assert.Equal(t, immediate, task.PrivateData.ResultDiscarded)
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
			if immediate {
				assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
				assert.Empty(t, task.Data)
				assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			} else {
				assert.Equal(t, model.TaskStatusNotStart, task.Status)
				assert.NotEmpty(t, task.Data)
				assert.Equal(t, model.BillingRecoveryStateTaskPending, recovery.ConsumptionState)
			}
			assert.EqualValues(t, 1, sends.Load())
		})
	}
}
