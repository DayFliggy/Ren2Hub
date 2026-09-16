package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGatewaySunoContinuation(t *testing.T, upstream http.HandlerFunc) (string, string, string, model.Task) {
	t.Helper()
	url, key, otherKey := setupGatewaySDKWithUpstream(t, upstream)
	require.NoError(t, model.DB.AutoMigrate(&model.Task{}))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"suno_music":0.001,"suno_lyrics":0.001}`))
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Type, channel.Models, channel.CapacityTotal = constant.ChannelTypeSunoAPI, "suno_music,suno_lyrics", 1
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"type": channel.Type, "models": channel.Models, "capacity_total": channel.CapacityTotal}).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	config := model.DefaultGatewayConfig()
	config.WaitMS = 0
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	code, body := postGatewayTask(t, url, key, "/suno/submit/music", `{"prompt":"local melody"}`)
	require.Equal(t, http.StatusOK, code, body)
	var task model.Task
	require.NoError(t, model.DB.First(&task).Error)
	require.Contains(t, body, task.TaskID)
	require.NotContains(t, body, "provider-suno-task")
	require.Equal(t, "provider-suno-task", task.GetUpstreamTaskID())
	require.Equal(t, 1, task.PrivateData.CredentialVersion)
	require.NotEmpty(t, task.PrivateData.CredentialID)
	require.Empty(t, task.PrivateData.Key)
	// Batch polling persists responseItem.Data as this same song array.
	task.Status = model.TaskStatusSuccess
	task.Data = []byte(`[{"id":"clip-one","status":"complete"},{"id":"clip-two","status":"complete"}]`)
	require.NoError(t, model.DB.Save(&task).Error)
	return url, key, otherKey, task
}

func gatewaySunoSubmitResponse(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"code":"success","message":"","data":"provider-suno-task"}`)
}

func TestGatewaySunoContinuationKeepsOwnerAndPrivateTaskID(t *testing.T) {
	var sends atomic.Int32
	url, key, _, task := setupGatewaySunoContinuation(t, func(w http.ResponseWriter, r *http.Request) {
		attempt := sends.Add(1)
		var request dto.SunoSubmitReq
		if err := common.DecodeJson(r.Body, &request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer local-test-key" || r.URL.Path != "/suno/submit/MUSIC" ||
			(attempt > 1 && (request.TaskID != "provider-suno-task" || request.ContinueClipId != "clip-two" || request.ContinueAt != 12.5)) {
			http.Error(w, "continuation owner or provider references changed", http.StatusBadRequest)
			return
		}
		gatewaySunoSubmitResponse(w)
	})
	channel, err := model.GetChannelById(task.ChannelId, true)
	require.NoError(t, err)
	channel.Key = "new-spare-key\nlocal-test-key"
	channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}
	require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"key": channel.Key, "channel_info": channel.ChannelInfo}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	body := fmt.Sprintf(`{"prompt":"continuation","task_id":%q,"continue_clip_id":"clip-two","continue_at":12.5}`, task.TaskID)
	code, response := postGatewayTask(t, url, key, "/suno/submit/music", body)
	require.Equal(t, http.StatusOK, code, response)
	assert.EqualValues(t, 2, sends.Load())
	var continuation model.Task
	require.NoError(t, model.DB.Where("id <> ?", task.ID).First(&continuation).Error)
	assert.Equal(t, task.ChannelId, continuation.ChannelId)
	assert.Equal(t, task.PrivateData.CredentialID, continuation.PrivateData.CredentialID)
	assert.Equal(t, 1, continuation.PrivateData.ChannelKeyIndex, "reordering must follow credential identity")
}

func TestGatewaySunoContinuationRejectsUnknownOwnershipBeforeSending(t *testing.T) {
	var sends atomic.Int32
	url, key, otherKey, task := setupGatewaySunoContinuation(t, func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		gatewaySunoSubmitResponse(w)
	})
	validBody := fmt.Sprintf(`{"task_id":%q,"continue_clip_id":"clip-one"}`, task.TaskID)
	for _, test := range []struct {
		name, token, path, body string
	}{
		{"missing-task", key, "music", `{"continue_clip_id":"clip-one"}`},
		{"missing-clip", key, "music", fmt.Sprintf(`{"task_id":%q}`, task.TaskID)},
		{"unknown-task", key, "music", `{"task_id":"task-unknown","continue_clip_id":"clip-one"}`},
		{"private-task-id", key, "music", `{"task_id":"provider-suno-task","continue_clip_id":"clip-one"}`},
		{"foreign-token", otherKey, "music", validBody},
		{"unknown-clip", key, "music", fmt.Sprintf(`{"task_id":%q,"continue_clip_id":"other-task-clip"}`, task.TaskID)},
		{"lyrics-continuation", key, "lyrics", validBody},
		{"null-body", key, "music", `null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			code, response := postGatewayTask(t, url, test.token, "/suno/submit/"+test.path, test.body)
			assert.Equal(t, http.StatusBadRequest, code, response)
		})
	}
	// Ownership failure must still be returned when no route could be selected.
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{1, 4}).Update("status", common.ChannelStatusManuallyDisabled).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	code, response := postGatewayTask(t, url, key, "/suno/submit/music", `{"task_id":"task-unknown","continue_clip_id":"clip-one"}`)
	assert.Equal(t, http.StatusBadRequest, code, response)
	assert.EqualValues(t, 1, sends.Load())
	assertSunoContinuationNoBilling(t, task)
}

func TestGatewaySunoContinuationRejectsInvalidTaskRecords(t *testing.T) {
	var sends atomic.Int32
	url, key, _, task := setupGatewaySunoContinuation(t, func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		gatewaySunoSubmitResponse(w)
	})
	for _, test := range []struct {
		name   string
		status int
		modify func(*model.Task)
	}{
		{"legacy-token", 400, func(task *model.Task) { task.PrivateData.TokenId = 0 }},
		{"wrong-platform", 400, func(task *model.Task) { task.Platform = "2" }},
		{"wrong-action", 400, func(task *model.Task) { task.Action = constant.SunoActionLyrics }},
		{"pending", 503, func(task *model.Task) { task.Status = model.TaskStatusInProgress }},
		{"invalid-song-data", 503, func(task *model.Task) { task.Data = []byte(`{"id":"clip-one"}`) }},
		{"legacy-credential", 503, func(task *model.Task) {
			task.PrivateData.CredentialVersion, task.PrivateData.CredentialID, task.PrivateData.EndpointID = 0, "", ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := task
			test.modify(&changed)
			require.NoError(t, model.DB.Save(&changed).Error)
			t.Cleanup(func() { require.NoError(t, model.DB.Save(&task).Error) })
			body := fmt.Sprintf(`{"task_id":%q,"continue_clip_id":"clip-one"}`, task.TaskID)
			code, response := postGatewayTask(t, url, key, "/suno/submit/music", body)
			assert.Equal(t, test.status, code, response)
		})
	}
	assert.EqualValues(t, 1, sends.Load())
	assertSunoContinuationNoBilling(t, task)
}

func TestGatewaySunoContinuationUnavailableOwnerDoesNotFallback(t *testing.T) {
	var sends atomic.Int32
	url, key, _, task := setupGatewaySunoContinuation(t, func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		gatewaySunoSubmitResponse(w)
	})
	body := fmt.Sprintf(`{"task_id":%q,"continue_clip_id":"clip-one"}`, task.TaskID)
	held, _, err := service.AcquireGatewayChannelPermit(context.Background(), "occupied-suno-origin", task.ChannelId, false, nil)
	require.NoError(t, err)
	code, response := postGatewayTask(t, url, key, "/suno/submit/music", body)
	assert.Equal(t, http.StatusTooManyRequests, code, response)
	held.Release()
	for _, update := range []map[string]any{
		{"status": common.ChannelStatusManuallyDisabled},
		{"status": common.ChannelStatusEnabled, "key": "replacement-key"},
		{"key": "local-test-key", "base_url": "http://127.0.0.1:1"},
	} {
		require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", task.ChannelId).Updates(update).Error)
		require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
		code, response = postGatewayTask(t, url, key, "/suno/submit/music", body)
		assert.Equal(t, http.StatusServiceUnavailable, code, response)
	}
	assert.EqualValues(t, 1, sends.Load())
	assertSunoContinuationNoBilling(t, task)
}

func TestGatewaySunoContinuationRechecksTokenModelPermission(t *testing.T) {
	var sends atomic.Int32
	url, key, _, task := setupGatewaySunoContinuation(t, func(w http.ResponseWriter, _ *http.Request) {
		sends.Add(1)
		gatewaySunoSubmitResponse(w)
	})
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", task.PrivateData.TokenId).Updates(map[string]any{
		"model_limits_enabled": true, "model_limits": "suno_lyrics",
	}).Error)
	body := fmt.Sprintf(`{"task_id":%q,"continue_clip_id":"clip-one"}`, task.TaskID)
	code, response := postGatewayTask(t, url, key, "/suno/submit/music", body)
	assert.GreaterOrEqual(t, code, http.StatusBadRequest, response)
	assert.EqualValues(t, 1, sends.Load())
	assertSunoContinuationNoBilling(t, task)
}

func assertSunoContinuationNoBilling(t *testing.T, task model.Task) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.First(&user, task.UserId).Error)
	assert.Equal(t, task.Quota, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	var reservations int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&reservations).Error)
	assert.EqualValues(t, 1, reservations)
	var failures int64
	require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
	assert.Zero(t, failures)
}
