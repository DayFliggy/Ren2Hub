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
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGatewayTask(t *testing.T, upstream http.HandlerFunc) (string, string, string) {
	t.Helper()
	_, key, otherKey := setupGatewaySDKWithUpstream(t, upstream)
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}, &model.TaskResultEvent{}, &model.PerfMetric{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}))
	require.NoError(t, controller.InitTaskPlugins())
	require.NoError(t, publishGatewayFixturePrices(`{"sora-2":0.001}`, false))
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

func assertGatewaySubmissionBilling(t *testing.T, expectedStatus string) {
	t.Helper()
	for pass := range 3 {
		if pass > 0 {
			_, err := service.RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+3600, 10)
			require.NoError(t, err)
		}
		var recovery model.BillingRecovery
		require.NoError(t, model.DB.First(&recovery).Error)
		assert.Equal(t, expectedStatus, recovery.Status)
		fundingHeld, tokenHeld := int(recovery.FundingAmount), int(recovery.TokenAmount)
		if expectedStatus == model.BillingRecoveryStatusRefunded {
			assert.Equal(t, model.BillingRecoveryStatusRefunded, recovery.Status)
			assert.NotEmpty(t, recovery.FinalEventID)
			fundingHeld, tokenHeld = 0, 0
		} else {
			assert.Equal(t, model.BillingRecoveryStateUsagePending, recovery.ConsumptionState)
			assert.Positive(t, fundingHeld)
			var refunds int64
			require.NoError(t, model.DB.Model(&model.BillingEvent{}).
				Where("request_id = ? AND operation = ?", recovery.RequestID, model.BillingRecoveryOperationRefund).Count(&refunds).Error)
			assert.Zero(t, refunds)
		}
		var user model.User
		var token model.Token
		require.NoError(t, model.DB.First(&user, recovery.UserID).Error)
		require.NoError(t, model.DB.First(&token, recovery.TokenID).Error)
		assert.EqualValues(t, 10000000-fundingHeld, user.Quota)
		assert.Zero(t, user.RequestCount)
		assert.Zero(t, user.UsedQuota)
		assert.Zero(t, token.RemainQuota, "unlimited token has no debited balance")
		assert.Zero(t, token.UsedQuota)
		assert.EqualValues(t, tokenHeld, token.ReservedQuota)
	}
}

func TestGatewayTaskAcceptedStatusesAndAccounting(t *testing.T) {
	for _, status := range []int{200, 201, 202, 299} {
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
			assert.Equal(t, model.BillingRecoveryStatusActive, recovery.Status)
			assert.Equal(t, model.BillingRecoveryStateTaskPending, recovery.ConsumptionState)
			var user model.User
			require.NoError(t, model.DB.First(&user, task.UserId).Error)
			assert.Zero(t, user.RequestCount)
			assert.Zero(t, user.UsedQuota)
			assert.EqualValues(t, task.Quota, user.ReservedQuota)
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
	assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusActive)
}

func TestGatewayTaskSettlementFailureKeepsTaskAndRecoversOnce(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("task_settle_failure", func(tx *gorm.DB) {
		if adjustment, ok := tx.Statement.Dest.(*model.BillingEvent); ok && adjustment.Operation == model.BillingRecoveryOperationSettle {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Create().Remove("task_settle_failure") })
	code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
	require.Equal(t, 200, code, body)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	assert.Contains(t, body, task.TaskID)
	from := task.Status
	task.Status, task.Progress = model.TaskStatusSuccess, "100%"
	won, err := model.PersistTaskCompletion(context.Background(), &task, from, task.Quota, model.BillingConsumption{Log: model.Log{UserId: task.UserId, TokenId: task.PrivateData.TokenId, ChannelId: task.ChannelId}, CountUsage: true, LogEnabled: true})
	require.NoError(t, err)
	require.True(t, won)
	_, err = service.RetryBillingRecovery(context.Background(), task.PrivateData.SubmissionRequestID)
	require.ErrorIs(t, err, assert.AnError)

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
	assert.False(t, ready, "completed accounting is not a pending task submission")
	var user model.User
	require.NoError(t, model.DB.First(&user, task.UserId).Error)
	assert.Equal(t, 1, user.RequestCount)
	assert.EqualValues(t, task.Quota, user.UsedQuota)
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
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "accepted without ID", status: 202, body: `{"object":"video","status":"queued"}`},
		{name: "accepted malformed JSON", status: 200, body: `{"id":`},
		{name: "server error after send", status: 500, body: `{"error":"upstream failed"}`},
		{name: "connection lost after send"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				if test.status == 0 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if assert.NoError(t, err) {
						assert.NoError(t, conn.Close())
					}
					return
				}
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			})
			code, _ := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
			assert.GreaterOrEqual(t, code, 400)
			assert.EqualValues(t, 1, sends.Load())
			assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusActive)
		})
	}
}

func TestGatewayTaskExplicitRejectionRefunds(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"invalid prompt","type":"invalid_request_error"}}`)
	})
	code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
	assert.Equal(t, http.StatusBadRequest, code, body)
	assert.EqualValues(t, 1, sends.Load())
	assertGatewaySubmissionBilling(t, model.BillingRecoveryStatusRefunded)
}

func TestGatewayTaskProviderRPMRejectsBeforeBilling(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	config := model.DefaultGatewayConfig()
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 4).Update("status", common.ChannelStatusManuallyDisabled).Error)
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
	started := time.Now()
	url, key, _ := setupGatewayTask(t, func(w http.ResponseWriter, r *http.Request) {
		if sends.Add(1) == 1 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"capacity exhausted before acceptance"}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"provider-job","status":"queued"}`)
	})
	code, body := postGatewayTask(t, url, key, "/v1/videos", `{"model":"sora-2","prompt":"local fixture"}`)
	require.Equal(t, 200, code, body)
	assert.EqualValues(t, 2, sends.Load())
	var health model.ChannelHealth
	require.NoError(t, model.DB.First(&health, "channel_id = ? AND cooldown_until > ?", 1, 0).Error)
	assert.GreaterOrEqual(t, health.CooldownUntil, started.Add(120*time.Second).Unix())
	rate, err := service.ReadGatewayProviderRateRuntime(context.Background(), common.RDB, service.GatewayChannelRateScopeID(4))
	require.NoError(t, err)
	assert.Zero(t, rate.CooldownMS, "A's cooldown must not propagate to the independent fallback")
}
