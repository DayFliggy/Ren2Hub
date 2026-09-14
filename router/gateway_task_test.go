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
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGatewayTask(t *testing.T, upstream http.HandlerFunc) (string, string, string) {
	t.Helper()
	_, key, otherKey := setupGatewaySDKWithUpstream(t, upstream)
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"sora-2":0.001}`))
	for _, id := range []int{1, 4} {
		ch, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		ch.Models = "sora-2"
		require.NoError(t, model.DB.Model(ch).Update("models", ch.Models).Error)
		require.NoError(t, ch.AddAbilities(model.DB))
	}
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetVideoRouter(engine)
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return server.URL, key, otherKey
}

func postGatewayTask(t *testing.T, url, key, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(data)
}

func TestGatewayTaskAcceptedStatusesAndAccounting(t *testing.T) {
	for _, status := range []int{200, 201, 202} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"id":"provider-job","object":"video","status":"queued","model":"sora-2"}`)
			})
			code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
			require.Equal(t, 200, code, body)
			assert.NotContains(t, body, "provider-job")
			var task model.Task
			require.NoError(t, model.DB.First(&task).Error)
			assert.Contains(t, body, task.TaskID)
			assert.Equal(t, "provider-job", task.PrivateData.UpstreamTaskID)
			assert.NotEmpty(t, task.PrivateData.CredentialID)
			assert.Empty(t, task.PrivateData.Key)
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery).Error)
			assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			assert.Equal(t, model.BillingRecoveryStateCompleted, recovery.ConsumptionState)
			var user model.User
			require.NoError(t, model.DB.First(&user, task.UserId).Error)
			assert.Equal(t, 1, user.RequestCount)
			assert.Equal(t, task.Quota, user.UsedQuota)
			assert.EqualValues(t, 1, sends.Load())
		})
	}
}

func TestGatewayTaskPersistenceFailureDoesNotExposeSuccessOrReplay(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("task_insert_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { require.NoError(t, model.DB.Callback().Create().Remove("task_insert_failure")) })
	code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
	assert.Equal(t, 503, code, body)
	assert.Contains(t, body, "task_persistence_failed")
	assert.NotContains(t, body, `"object":"video"`)
	assert.NotContains(t, body, "provider-job")
	assert.EqualValues(t, 1, sends.Load())
	var count int64
	require.NoError(t, model.DB.Model(&model.Task{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestGatewayTaskSettlementFailureKeepsTaskAndRecoversOnce(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("task_settle_failure", func(tx *gorm.DB) {
		if adjustment, ok := tx.Statement.Dest.(*model.BillingRecoveryAdjustment); ok && adjustment.Operation == model.BillingRecoveryOperationSettle {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Create().Remove("task_settle_failure") })
	code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
	require.Equal(t, 200, code, body)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.Contains(t, body, task.TaskID)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettlementPending, recovery.Status)
	ready, err := model.TaskSubmissionReady(context.Background(), &task)
	require.NoError(t, err)
	assert.False(t, ready, "polling cannot refund or adjust unfinished submission accounting")
	require.NoError(t, model.DB.Callback().Create().Remove("task_settle_failure"))
	for range 2 {
		_, err := service.RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+3600, 10)
		require.NoError(t, err)
	}
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", recovery.RequestID).Error)
	assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
	assert.Equal(t, model.BillingRecoveryStateCompleted, recovery.ConsumptionState)
	ready, err = model.TaskSubmissionReady(context.Background(), &task)
	require.NoError(t, err)
	assert.True(t, ready)
	var user model.User
	require.NoError(t, model.DB.First(&user, task.UserId).Error)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, task.Quota, user.UsedQuota)
	assert.EqualValues(t, 1, sends.Load())
}

func TestGatewayTaskRemixKeepsOriginalCredentialAndProviderID(t *testing.T) {
	var sends atomic.Int32
	url, key, otherKey := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		attempt := sends.Add(1)
		if r.Header.Get("Authorization") != "Bearer local-test-key" || attempt > 1 && r.URL.Path != "/v1/videos/provider-job/remix" {
			http.Error(w, "task owner or provider ID changed", http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
	require.Equal(t, 200, code, body)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	ch, err := model.GetChannelById(task.ChannelId, true)
	require.NoError(t, err)
	ch.Key = "new-spare-key\nlocal-test-key"
	ch.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}
	require.NoError(t, model.DB.Model(ch).Updates(map[string]any{"key": ch.Key, "channel_info": ch.ChannelInfo}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	path := "/v1/videos/" + task.TaskID + "/remix"
	code, body = postGatewayTask(t, url, key, path, `{"prompt":"continue local fixture"}`)
	require.Equal(t, 200, code, body)
	assert.EqualValues(t, 2, sends.Load())
	code, body = postGatewayTask(t, url, otherKey, path, `{"prompt":"continue local fixture"}`)
	assert.Equal(t, 400, code, body)
	assert.EqualValues(t, 2, sends.Load())
	ch.Key = "replacement-key"
	ch.ChannelInfo = model.ChannelInfo{}
	require.NoError(t, model.DB.Model(ch).Updates(map[string]any{"key": ch.Key, "channel_info": ch.ChannelInfo, "status": common.ChannelStatusEnabled}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	code, body = postGatewayTask(t, url, key, path, `{"prompt":"continue local fixture"}`)
	assert.Equal(t, 503, code, body)
	assert.EqualValues(t, 2, sends.Load())
}

func TestGatewayTaskUnknownAcceptanceIsNotReplayed(t *testing.T) {
	for _, status := range []int{202, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"accepted status without a usable task ID"}`)
			})
			code, _ := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
			assert.GreaterOrEqual(t, code, 400)
			assert.EqualValues(t, 1, sends.Load())
		})
	}
}

func TestGatewayTaskProviderRPMRejectsBeforeBilling(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	config := model.DefaultGatewayConfig()
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	config.ChannelRates[1] = model.GatewayProviderRateLimit{RPM: 1}
	require.NoError(t, model.SaveGatewayConfig(config))
	for index := range 2 {
		code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
		if index == 0 {
			require.Equal(t, 200, code, body)
		} else {
			require.Equal(t, 429, code, body)
		}
	}
	assert.EqualValues(t, 1, sends.Load())
	var recoveries int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
	assert.EqualValues(t, 1, recoveries, "rate rejection must not create another billing reservation")
	var failures int64
	require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
	assert.Zero(t, failures, "local RPM rejection must not penalize the provider")
}

func TestGatewayTaskExplicitRejectionCanAdvance(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		if sends.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"capacity exhausted before acceptance"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
	require.Equal(t, 200, code, body)
	assert.EqualValues(t, 2, sends.Load())
}
