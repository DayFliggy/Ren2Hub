package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGatewayMidjourney(t *testing.T, upstream http.HandlerFunc) (string, string, string) {
	t.Helper()
	url, key, other := setupGatewaySDKWithUpstream(t, upstream)
	require.NoError(t, model.DB.AutoMigrate(&model.Midjourney{}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.001,"mj_upscale":0.001,"mj_upload":0.001,"swap_face":0.001}`))
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type = constant.ChannelTypeMidjourneyPlus
		channel.Models = "mj_imagine,mj_upscale,mj_upload,swap_face"
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	return url, key, other
}

func TestGatewayMidjourneyAcceptedSubmissionAccounting(t *testing.T) {
	for _, proxyCode := range []int{1, 21, 22} {
		t.Run(fmt.Sprint(proxyCode), func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "local-test-key", r.Header.Get("mj-api-secret"))
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.Less(t, user.Quota, 10000000, "reservation must precede the actual send")
				}
				_, _ = fmt.Fprintf(w, `{"code":%d,"result":"provider-mj","description":"queued","extension":false}`, proxyCode)
			})
			code, body := postGatewayTask(t, url, key, "/mj/submit/imagine", `{"prompt":"local fixture"}`)
			require.Equal(t, 200, code, body)
			assert.Contains(t, body, `"code":1`)
			assert.Contains(t, body, `"extension":false`)
			var task model.Midjourney
			require.NoError(t, model.DB.First(&task).Error)
			assert.Equal(t, "provider-mj", task.MjId)
			assert.NotEmpty(t, task.PrivateData.CredentialID)
			assert.NotEmpty(t, task.PrivateData.EndpointID)
			assert.Empty(t, task.PrivateData.Key)
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
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

func TestGatewayMidjourneyUnknownAcceptanceDoesNotReplay(t *testing.T) {
	for index, response := range []string{"", `{}`, `{"code":1}`, `{"code":1,"result":[]}`, `{"code":1,"result":""}`} {
		t.Run(fmt.Sprintf("body_%d", index), func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				_, _ = io.WriteString(w, response)
			})
			code, body := postGatewayTask(t, url, key, "/mj/submit/imagine", `{"prompt":"local fixture"}`)
			assert.Equal(t, 502, code, body)
			assert.EqualValues(t, 1, sends.Load())
			var count int64
			require.NoError(t, model.DB.Model(&model.Midjourney{}).Count(&count).Error)
			assert.Zero(t, count)
			require.Eventually(t, func() bool {
				var recovery model.BillingRecovery
				return model.DB.First(&recovery).Error == nil && recovery.Status == model.BillingRecoveryStatusRefunded
			}, time.Second, 10*time.Millisecond)
		})
	}
}

func TestGatewayMidjourneyPersistenceAndSettlementRecovery(t *testing.T) {
	for _, failure := range []string{"task", "settlement", "log"} {
		t.Run(failure, func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				_, _ = io.WriteString(w, `{"code":1,"result":"provider-mj"}`)
			})
			require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("mj_fault", func(tx *gorm.DB) {
				if failure == "task" && tx.Statement.Table == "midjourneys" || failure == "log" && tx.Statement.Table == "logs" {
					tx.AddError(assert.AnError)
				}
				if adjustment, ok := tx.Statement.Dest.(*model.BillingRecoveryAdjustment); ok && failure == "settlement" && adjustment.Operation == model.BillingRecoveryOperationSettle {
					tx.AddError(assert.AnError)
				}
			}))
			t.Cleanup(func() { _ = model.DB.Callback().Create().Remove("mj_fault") })
			code, body := postGatewayTask(t, url, key, "/mj/submit/imagine", `{"prompt":"local fixture"}`)
			if failure == "task" {
				assert.Equal(t, 503, code, body)
				assert.NotContains(t, body, "provider-mj")
				assert.Contains(t, body, "do not resubmit")
			} else {
				require.Equal(t, 200, code, body)
				var task model.Midjourney
				require.NoError(t, model.DB.First(&task).Error)
				ready, err := model.MidjourneySubmissionReady(context.Background(), &task)
				require.NoError(t, err)
				assert.False(t, ready)
			}
			require.NoError(t, model.DB.Callback().Create().Remove("mj_fault"))
			for range 2 {
				_, err := service.RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+3600, 10)
				require.NoError(t, err)
			}
			assert.EqualValues(t, 1, sends.Load())
			if failure != "task" {
				var task model.Midjourney
				require.NoError(t, model.DB.First(&task).Error)
				ready, err := model.MidjourneySubmissionReady(context.Background(), &task)
				require.NoError(t, err)
				assert.True(t, ready)
				var user model.User
				require.NoError(t, model.DB.First(&user, task.UserId).Error)
				assert.Equal(t, 1, user.RequestCount)
				assert.Equal(t, task.Quota, user.UsedQuota)
			}
		})
	}
}

func TestGatewayMidjourneyContinuationCredentialAndToken(t *testing.T) {
	var sends atomic.Int32
	url, key, otherKey := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
		attempt := sends.Add(1)
		assert.Equal(t, "local-test-key", r.Header.Get("mj-api-secret"))
		if attempt == 1 {
			_, _ = io.WriteString(w, `{"code":1,"result":"provider-mj"}`)
		} else {
			_, _ = io.WriteString(w, `{"code":1,"result":"provider-followup"}`)
		}
	})
	code, body := postGatewayTask(t, url, key, "/mj/submit/imagine", `{"prompt":"local fixture"}`)
	require.Equal(t, 200, code, body)
	var task model.Midjourney
	require.NoError(t, model.DB.First(&task).Error)
	require.NoError(t, model.DB.Model(&task).Updates(map[string]any{"status": "SUCCESS", "progress": "100%"}).Error)
	channel, err := model.GetChannelById(task.ChannelId, true)
	require.NoError(t, err)
	channel.Key, channel.ChannelInfo = "spare-key\nlocal-test-key", model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"key": channel.Key, "channel_info": channel.ChannelInfo}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	code, body = postGatewayTask(t, url, key, "/mj/submit/simple-change", `{"content":"provider-mj u1"}`)
	require.Equal(t, 200, code, body)
	assert.EqualValues(t, 2, sends.Load())
	code, body = postGatewayTask(t, url, otherKey, "/mj/submit/simple-change", `{"content":"provider-mj u1"}`)
	assert.Equal(t, 400, code, body)
	assert.EqualValues(t, 2, sends.Load())
	request, err := http.NewRequest(http.MethodGet, url+"/mj/task/provider-mj/fetch", nil)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+otherKey)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	response.Body.Close()
	assert.Equal(t, 400, response.StatusCode)
	code, body = postGatewayTask(t, url, otherKey, "/mj/task/list-by-condition", `{"ids":["provider-mj"]}`)
	assert.Equal(t, 400, code, body)
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"key": "replacement", "channel_info": model.ChannelInfo{}}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	code, body = postGatewayTask(t, url, key, "/mj/submit/simple-change", `{"content":"provider-mj u1"}`)
	assert.Equal(t, 503, code, body)
	assert.EqualValues(t, 2, sends.Load())
}

func TestGatewayMidjourneyUploadPreservesArrayAndExtensions(t *testing.T) {
	url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":1,"result":["https://fixture.invalid/image"],"extension":false}`)
	})
	code, body := postGatewayTask(t, url, key, "/mj/submit/upload-discord-images", `{"base64Array":["fixture"]}`)
	require.Equal(t, 200, code, body)
	assert.Contains(t, body, `"result":["https://fixture.invalid/image"]`)
	var task model.Midjourney
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, "SUCCESS", task.Status)
	assert.True(t, strings.HasPrefix(task.MjId, "task_"))
}

func TestGatewayMidjourneyExplicitRejectionCanAdvance(t *testing.T) {
	var sends atomic.Int32
	started := time.Now()
	url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
		if sends.Add(1) == 1 {
			w.Header().Set("Retry-After", "120")
			_, _ = io.WriteString(w, `{"code":23,"description":"queue full before acceptance"}`)
			return
		}
		assert.Equal(t, "local-fallback-key", r.Header.Get("mj-api-secret"))
		_, _ = io.WriteString(w, `{"code":1,"result":"fallback-job"}`)
	})
	code, body := postGatewayTask(t, url, key, "/mj/submit/imagine", `{"prompt":"fixture"}`)
	require.Equal(t, 200, code, body)
	assert.EqualValues(t, 2, sends.Load())
	var tasks []model.Midjourney
	require.NoError(t, model.DB.Find(&tasks).Error)
	require.Len(t, tasks, 1)
	assert.Equal(t, 4, tasks[0].ChannelId)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, tasks[0].Quota, user.UsedQuota)
	var health model.ChannelHealth
	require.NoError(t, model.DB.First(&health, "channel_id = ? AND cooldown_until > ?", 1, 0).Error)
	assert.GreaterOrEqual(t, health.CooldownUntil, started.Add(120*time.Second).Unix())
}

func TestGatewayMidjourneyProviderRPMDoesNotPrechargeOrPenalize(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `{"code":1,"result":"provider-mj"}`)
	})
	config := model.DefaultGatewayConfig()
	// This case isolates a fully unavailable pool; independent fallback is
	// covered separately by the channel failover tests.
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 4).Update("status", common.ChannelStatusManuallyDisabled).Error)
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	config.ChannelRates[1] = model.GatewayProviderRateLimit{RPM: 1}
	require.NoError(t, model.SaveGatewayConfig(config))
	for index := range 2 {
		code, body := postGatewayTask(t, url, key, "/mj/submit/imagine", `{"prompt":"fixture"}`)
		if index == 0 {
			require.Equal(t, 200, code, body)
		} else {
			assert.Equal(t, 429, code, body)
		}
	}
	assert.EqualValues(t, 1, sends.Load())
	var reservations, failures int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&reservations).Error)
	require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
	assert.EqualValues(t, 1, reservations)
	assert.Zero(t, failures)
}

func TestGatewayMidjourneySubscriptionSubmissionAndTerminalRefund(t *testing.T) {
	url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"code":1,"result":"subscription-job"}`)
	})
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{
		"quota": 0, "setting": `{"billing_preference":"subscription_only"}`,
	}).Error)
	plan := model.SubscriptionPlan{Title: "isolated MJ", TotalAmount: 10000, Enabled: true, QuotaResetPeriod: "never"}
	require.NoError(t, model.DB.Create(&plan).Error)
	subscription := model.UserSubscription{UserId: 1, PlanId: plan.Id, AmountTotal: 10000, Status: "active", StartTime: time.Now().Unix(), EndTime: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, model.DB.Create(&subscription).Error)
	code, body := postGatewayTask(t, url, key, "/mj/submit/imagine", `{"prompt":"fixture"}`)
	require.Equal(t, 200, code, body)
	var task model.Midjourney
	require.NoError(t, model.DB.First(&task).Error)
	assert.Equal(t, service.BillingSourceSubscription, task.PrivateData.BillingSource)
	require.NoError(t, model.DB.First(&subscription, subscription.Id).Error)
	assert.EqualValues(t, task.Quota, subscription.AmountUsed)
	from := task.Status
	task.Status, task.Progress, task.FailReason = "FAILURE", "100%", "provider rejected job"
	won, err := service.CompleteMidjourneyBilling(context.Background(), &task, from)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = service.CompleteMidjourneyBilling(context.Background(), &task, from)
	require.NoError(t, err)
	assert.False(t, won)
	require.NoError(t, model.DB.First(&subscription, subscription.Id).Error)
	assert.Zero(t, subscription.AmountUsed)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Zero(t, user.Quota)
	assert.Zero(t, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
}

func TestGatewayMidjourneyFourthAvailableCandidateAndCancellation(t *testing.T) {
	var sends atomic.Int32
	started, stopped := make(chan struct{}), make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)
	url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		assert.Equal(t, "fourth-key", r.Header.Get("mj-api-secret"))
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-finish:
		}
	})
	base, err := model.GetChannelById(1, true)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		channel := *base
		channel.Id, channel.Key, channel.Name = 0, "blocked-extra", fmt.Sprintf("extra-%d", i)
		if i == 1 {
			channel.Key = "fourth-key"
			channel.Priority = common.GetPointer(int64(-2))
		}
		require.NoError(t, model.DB.Create(&channel).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
		if i == 0 {
			for slot := 0; slot < channel.CapacityTotal; slot++ {
				permit, _, err := service.AcquireGatewayChannelPermit(context.Background(), fmt.Sprintf("extra-%d", slot), channel.Id, false, nil)
				defer permit.Release()
				require.NoError(t, err)
			}
		}
	}
	for _, id := range []int{1, 4} {
		for slot := 0; slot < 10; slot++ {
			name := fmt.Sprintf("held-%d-%d", id, slot)
			permit, _, err := service.AcquireGatewayChannelPermit(context.Background(), name, id, false, nil)
			defer permit.Release()
			require.NoError(t, err)
		}
	}
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/mj/submit/imagine", strings.NewReader(`{"prompt":"fixture"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	done := make(chan error, 1)
	go func() {
		response, err := http.DefaultClient.Do(request)
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("request ended before fourth candidate: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("fourth candidate was not reached")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("client cancellation did not reach upstream")
	}
	<-done
	assert.EqualValues(t, 1, sends.Load())
}

func TestGatewayMidjourneyKnownAcceptancePersistsAfterClientCancellation(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `{"code":1,"result":"accepted-before-cancel"}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("mj_cancel_after_acceptance", func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			cancel()
		}
	}))
	t.Cleanup(func() { _ = model.DB.Callback().Create().Remove("mj_cancel_after_acceptance") })
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/mj/submit/imagine", strings.NewReader(`{"prompt":"fixture"}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if response != nil {
		response.Body.Close()
	}
	require.ErrorIs(t, err, context.Canceled)
	var task model.Midjourney
	require.Eventually(t, func() bool {
		var recovery model.BillingRecovery
		return model.DB.First(&task).Error == nil && model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error == nil && recovery.Status == model.BillingRecoveryStatusSettled && recovery.ConsumptionState == model.BillingRecoveryStateCompleted
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, "accepted-before-cancel", task.MjId)
	assert.NotEmpty(t, task.PrivateData.CredentialID)
	var user model.User
	require.NoError(t, model.DB.First(&user, 1).Error)
	assert.Equal(t, 1, user.RequestCount)
	assert.Equal(t, task.Quota, user.UsedQuota)
	assert.Equal(t, 10000000-task.Quota, user.Quota)
	assert.EqualValues(t, 1, sends.Load())
}

func TestGatewayMidjourneyMalformedActionsRejectBeforeSend(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewayMidjourney(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1) })
	for _, test := range []struct{ path, body string }{
		{"/mj/submit/action", `{"customId":"MJ"}`},
		{"/mj/submit/action", `{"customId":"MJ::JOB::upsample"}`},
		{"/mj/submit/simple-change", `{"content":"task u"}`},
		{"/mj/submit/simple-change", `{"content":"task u100"}`},
	} {
		code, _ := postGatewayTask(t, url, key, test.path, test.body)
		assert.Equal(t, 400, code)
	}
	assert.Zero(t, sends.Load())
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&count).Error)
	assert.Zero(t, count)
}
