package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVideoTaskLifecycle(t *testing.T) *gorm.DB {
	t.Setenv("CRYPTO_SECRET", "video-lifecycle-test-secret")
	t.Helper()
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}, &model.Task{}, &model.Token{}, &model.BillingRecovery{}, &model.BillingAttempt{}, &model.BillingEvent{}, &model.QuotaData{}, &model.BillingPriceVersion{}))
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	previousBatch, previousRedis, previousLogging := common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled
	common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = false, false, true
	previousTimeout, previousLimit := constant.TaskTimeoutMinutes, constant.TaskQueryLimit
	constant.TaskTimeoutMinutes, constant.TaskQueryLimit = 0, 20
	previousFactory := service.GetTaskAdaptorFunc
	t.Cleanup(func() {
		common.BatchUpdateEnabled, common.RedisEnabled, common.LogConsumeEnabled = previousBatch, previousRedis, previousLogging
		constant.TaskTimeoutMinutes, constant.TaskQueryLimit = previousTimeout, previousLimit
		service.GetTaskAdaptorFunc = previousFactory
	})
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"available_quota": 100, "used_quota": 0, "request_count": 0}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "video-test-token", RemainQuota: 100}).Error)
	require.NoError(t, InitTaskPlugins())
	service.InitHttpClient()
	return db
}

func seedBoundVideoTask(t *testing.T, db *gorm.DB, channelType int, endpoint string, status model.TaskStatus) (*model.Channel, *model.Task) {
	t.Helper()
	channel := &model.Channel{Type: channelType, Name: "video-owner", Key: "sk-original\nsk-other", BaseURL: &endpoint,
		Status: common.ChannelStatusEnabled, ChannelInfo: model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}}
	require.NoError(t, db.Create(channel).Error)
	task := &model.Task{TaskID: "task_video_owner", UserId: 1, ChannelId: channel.Id, Platform: constant.TaskPlatform(fmt.Sprint(channelType)),
		Action: constant.TaskActionGenerate, Status: status, Progress: "30%", Quota: 20, SubmitTime: time.Now().Unix(),
		Properties: model.Properties{OriginModelName: "video-fixture"}}
	require.NoError(t, task.PrivateData.BindCredential(context.Background(), channel, "sk-original"))
	task.PrivateData.UpstreamTaskID, task.PrivateData.TokenId = "provider-job", 1
	task.PrivateData.BillingSource = service.BillingSourceWallet
	task.PrivateData.SubmissionRequestID = "video-submit"
	sora, found := jsplugin.DefaultRegistry.Get("sora")
	require.True(t, found)
	version, err := model.GetTaskPluginVersion("sora", sora.Meta.Version)
	require.NoError(t, err)
	ref := model.TaskPluginRef{Key: version.Key, Version: version.Version, SourceHash: version.SourceHash}
	task.Platform = "sora"
	task.PrivateData.PluginRef = &ref
	task.PrivateData.PluginAttemptID = "video-attempt"
	task.Data = []byte("{}")
	definition, err := billing.NewModelPriceVersion("video-fixture", billing.PricingValues{"ModelPrice": "0.00004"}, false)
	require.NoError(t, err)
	saved, err := model.StoreBillingPriceEvidence(t.Context(), definition)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*saved, "1", nil, time.Unix(1700000000, 0))
	require.NoError(t, err)
	estimated, charge, err := model.EvaluateBilling(quote, billing.Usage{Quantities: map[string]string{"calls": "1"}, Source: "request_estimate", Estimated: true}, billingexpr.RequestInput{}, task.PrivateData.SubmissionRequestID, task.PrivateData.PluginAttemptID, "estimate")
	require.NoError(t, err)
	task.PrivateData.BillingContext = &model.TaskBillingContext{AttemptID: task.PrivateData.PluginAttemptID, Quote: quote, Usage: &estimated, Charge: charge}
	_, err = model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: task.PrivateData.SubmissionRequestID, UserID: 1, TokenID: 1, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, model.Reserve(task.PrivateData.SubmissionRequestID, 20))
	require.NoError(t, model.PrepareBillingAttemptWithPlugin(context.Background(), task.PrivateData.SubmissionRequestID, task.PrivateData.PluginAttemptID, task.ChannelId, quote, &ref))
	require.NoError(t, model.PersistBillingEstimate(task.PrivateData.PluginAttemptID, estimated, charge))
	require.NoError(t, model.PersistAcceptedTask(task, task.PrivateData.SubmissionRequestID, model.BillingConsumption{AttemptID: task.PrivateData.PluginAttemptID, Usage: &estimated, Charge: charge, Log: model.Log{UserId: 1, TokenId: 1, ChannelId: channel.Id, Quota: 20}, CountUsage: true, LogEnabled: true}))
	return channel, task
}

func assertVideoTaskAccountingUnchanged(t *testing.T, db *gorm.DB, channelID int, settled ...bool) {
	t.Helper()
	var user model.User
	var token model.Token
	var channel model.Channel
	require.NoError(t, db.First(&user, 1).Error)
	require.NoError(t, db.First(&token, 1).Error)
	require.NoError(t, db.First(&channel, channelID).Error)
	used, reserved, requests := 0, 20, 0
	if len(settled) > 0 && settled[0] {
		used, reserved, requests = 20, 0, 1
	}
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, used, user.UsedQuota)
	assert.EqualValues(t, reserved, user.ReservedQuota)
	assert.Equal(t, requests, user.RequestCount)
	assert.EqualValues(t, 80, token.RemainQuota)
	assert.EqualValues(t, used, token.UsedQuota)
	assert.EqualValues(t, reserved, token.ReservedQuota)
	assert.EqualValues(t, used, channel.UsedQuota)
}

func TestVideoContentUsesOriginalTaskCredential(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeSora} {
		for _, scenario := range []string{"reordered", "replaced", "endpoint-changed", "disabled"} {
			t.Run(fmt.Sprintf("%d/%s", channelType, scenario), func(t *testing.T) {
				db := setupVideoTaskLifecycle(t)
				fetchSettings := system_setting.GetFetchSetting()
				previousProtection := fetchSettings.EnableSSRFProtection
				fetchSettings.EnableSSRFProtection = false
				t.Cleanup(func() { fetchSettings.EnableSSRFProtection = previousProtection })
				var sends atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					assert.Equal(t, "/v1/videos/provider-job/content", r.URL.Path)
					assert.Equal(t, "Bearer sk-original", r.Header.Get("Authorization"))
					w.Header().Set("Content-Type", "video/mp4")
					_, _ = io.WriteString(w, "video-fixture")
				}))
				defer upstream.Close()
				channel, task := seedBoundVideoTask(t, db, channelType, upstream.URL, model.TaskStatusSuccess)
				updates := map[string]any{"key": "sk-other\nsk-original"}
				switch scenario {
				case "replaced":
					updates["key"] = "sk-other\nsk-replacement"
				case "endpoint-changed":
					updates["base_url"] = upstream.URL + "/new-account"
				case "disabled":
					updates["status"] = common.ChannelStatusManuallyDisabled
				}
				require.NoError(t, db.Model(channel).Updates(updates).Error)
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/"+task.TaskID+"/content", nil)
				c.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
				c.Set("id", 1)
				VideoProxy(c)
				if scenario == "reordered" {
					assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
					assert.Equal(t, "video-fixture", recorder.Body.String())
					assert.EqualValues(t, 1, sends.Load())
				} else {
					assert.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
					assert.Contains(t, recorder.Body.String(), "artifact_plugin_unavailable")
					assert.Zero(t, sends.Load())
				}
				assertVideoTaskAccountingUnchanged(t, db, channel.Id)
				var recoveries int64
				require.NoError(t, db.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				assert.EqualValues(t, 1, recoveries)
			})
		}
	}
}
