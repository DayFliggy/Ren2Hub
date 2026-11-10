package middleware

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupOriginTaskDB(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.AutoMigrate(&model.Task{}, &model.Channel{}, &model.Ability{}))
	model.DB = database
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
	})
}

func insertOriginTaskChannel(t *testing.T, status int) *model.Channel {
	t.Helper()
	channel := &model.Channel{
		Name:   "origin-channel",
		Models: "resolved-model",
		Key:    "sk-origin",
		Status: status,
		Type:   constant.ChannelTypeDoubaoVideo,
	}
	require.NoError(t, model.DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	return channel
}

func insertOriginOwnedTask(t *testing.T, taskID string, userID, channelID int, platform constant.TaskPlatform) *model.Task {
	t.Helper()
	task := &model.Task{
		TaskID:    taskID,
		UserId:    userID,
		ChannelId: channelID,
		Platform:  platform,
		Action:    "text_to_video",
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-" + taskID,
		},
	}
	data, err := common.Marshal(map[string]any{"id": "upstream-" + taskID})
	require.NoError(t, err)
	task.Data = data
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func originTaskTestContext(userID int) *gin.Context {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", nil)
	common.SetContextKey(c, constant.ContextKeyUserId, userID)
	return c
}

func resolvedOriginPin(c *gin.Context) (int, bool) {
	pin, found, _ := service.GetChannelConstraints(c).ResolvedPin()
	if !found {
		return 0, false
	}
	return pin.ChannelId, true
}

func TestApplyOriginTaskIntent(t *testing.T) {
	setupOriginTaskDB(t)
	enabled := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
	otherEnabled := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
	disabled := insertOriginTaskChannel(t, common.ChannelStatusManuallyDisabled)
	insertOriginOwnedTask(t, "task-own", 7, enabled.Id, "origin-plugin")
	insertOriginOwnedTask(t, "task-own-b", 7, enabled.Id, "origin-plugin")
	insertOriginOwnedTask(t, "task-other-channel", 7, otherEnabled.Id, "origin-plugin")
	insertOriginOwnedTask(t, "task-foreign", 8, enabled.Id, "origin-plugin")
	insertOriginOwnedTask(t, "task-wrong-platform", 7, enabled.Id, "other-plugin")
	insertOriginOwnedTask(t, "task-legacy", 7, enabled.Id, constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeDoubaoVideo)))
	insertOriginOwnedTask(t, "task-disabled", 7, disabled.Id, "origin-plugin")

	tooMany := make([]any, 17)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("task-%d", i)
	}

	tests := []struct {
		name        string
		userID      int
		intent      map[string]any
		channelType int
		wantCode    string
		wantPinned  int
		wantIDs     []string
	}{
		{
			name:       "valid single origin id pins channel",
			userID:     7,
			intent:     map[string]any{"originTaskIds": []any{"task-own"}},
			wantPinned: enabled.Id,
			wantIDs:    []string{"task-own"},
		},
		{
			name:       "dedupes preserving first order",
			userID:     7,
			intent:     map[string]any{"originTaskIds": []any{"task-own", " task-own ", "task-own-b"}},
			wantPinned: enabled.Id,
			wantIDs:    []string{"task-own", "task-own-b"},
		},
		{
			name:        "legacy platform matches channelType",
			userID:      7,
			intent:      map[string]any{"originTaskIds": []any{"task-legacy"}},
			channelType: constant.ChannelTypeDoubaoVideo,
			wantPinned:  enabled.Id,
			wantIDs:     []string{"task-legacy"},
		},
		{
			name:     "unknown id",
			userID:   7,
			intent:   map[string]any{"originTaskIds": []any{"task-missing"}},
			wantCode: "origin_task_not_found",
		},
		{
			name:     "other user's task is not found",
			userID:   7,
			intent:   map[string]any{"originTaskIds": []any{"task-foreign"}},
			wantCode: "origin_task_not_found",
		},
		{
			name:     "platform mismatch",
			userID:   7,
			intent:   map[string]any{"originTaskIds": []any{"task-wrong-platform"}},
			wantCode: "origin_task_platform_mismatch",
		},
		{
			name:     "two ids on different channels",
			userID:   7,
			intent:   map[string]any{"originTaskIds": []any{"task-own", "task-other-channel"}},
			wantCode: "origin_task_channel_conflict",
		},
		{
			name:     "disabled channel",
			userID:   7,
			intent:   map[string]any{"originTaskIds": []any{"task-disabled"}},
			wantCode: "origin_task_channel_disabled",
		},
		{
			name:     "more than 16 ids",
			userID:   7,
			intent:   map[string]any{"originTaskIds": tooMany},
			wantCode: "invalid_origin_task_ids",
		},
		{
			name:     "non-array originTaskIds",
			userID:   7,
			intent:   map[string]any{"originTaskIds": "task-own"},
			wantCode: "invalid_origin_task_ids",
		},
		{
			name:     "empty string entry",
			userID:   7,
			intent:   map[string]any{"originTaskIds": []any{"task-own", "  "}},
			wantCode: "invalid_origin_task_ids",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			c := originTaskTestContext(testCase.userID)
			intentErr := applyOriginTaskIntent(c, testCase.intent, jsplugin.Meta{Key: "origin-plugin", ChannelTypes: []int{testCase.channelType}})
			if testCase.wantCode != "" {
				require.NotNil(t, intentErr)
				assert.Equal(t, testCase.wantCode, intentErr.Code)
				assert.Equal(t, http.StatusBadRequest, intentErr.StatusCode)
				_, pinned := resolvedOriginPin(c)
				assert.False(t, pinned)
				return
			}
			require.Nil(t, intentErr)
			pinnedID, ok := resolvedOriginPin(c)
			require.True(t, ok)
			assert.Equal(t, testCase.wantPinned, pinnedID)
			tasks, ok := common.GetContextKeyType[[]*model.Task](c, constant.ContextKeyOriginTasks)
			require.True(t, ok)
			require.Len(t, tasks, len(testCase.wantIDs))
			for i, wantID := range testCase.wantIDs {
				assert.Equal(t, wantID, tasks[i].TaskID)
			}
		})
	}
}

func TestApplyOriginTaskIntentAbsentAndEmptyAreNoop(t *testing.T) {
	setupOriginTaskDB(t)
	c := originTaskTestContext(7)
	require.Nil(t, applyOriginTaskIntent(c, map[string]any{}, jsplugin.Meta{Key: "origin-plugin"}))
	require.Nil(t, applyOriginTaskIntent(c, map[string]any{"originTaskIds": []any{}}, jsplugin.Meta{Key: "origin-plugin"}))
	_, pinned := resolvedOriginPin(c)
	assert.False(t, pinned)
}

func TestPrepareTaskPluginRoutePinsOriginTaskChannel(t *testing.T) {
	setupOriginTaskDB(t)
	channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
	insertOriginOwnedTask(t, "task-route", 7, channel.Id, "origin-route")
	plugin := compileTaskRoutePlugin(t, `
export const meta = {
  apiVersion: 1, key: "origin-route", name: "Origin", version: "1.0.0",
  author: {name: "Test"},
  models: ["resolved-model"], fetchMode: "per_task",
  routes: [{method: "POST", path: "/vendor/jobs", type: "submit", decode: "decodeJob", render: "jobCreated"}],
};
export const native = {
  decodeJob: function() { return {kind: "submit", model: "resolved-model", originTaskIds: ["task-route"], requestBody: {prompt: "ok"}}; },
  jobCreated: function(ctx, task) { return task; },
};
export function buildSubmitRequest() { return {url: "https://example.com"}; }
export function parseSubmitResponse() { return {taskId: "one"}; }
export function buildQueryRequest() { return {url: "https://example.com"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`)
	reached := false
	router := gin.New()
	router.POST("/vendor/jobs", pinTaskPluginRoute(plugin, 0), func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserId, 7)
		c.Next()
	}, PrepareTaskPluginRoute(), func(c *gin.Context) {
		reached = true
		pinnedID, ok := resolvedOriginPin(c)
		require.True(t, ok)
		assert.Equal(t, channel.Id, pinnedID)
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{"model":"resolved-model"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.True(t, reached)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestPrepareTaskPluginRouteRejectsUnknownOriginTask(t *testing.T) {
	setupOriginTaskDB(t)
	plugin := compileTaskRoutePlugin(t, `
export const meta = {
  apiVersion: 1, key: "origin-route-missing", name: "Origin", version: "1.0.0",
  author: {name: "Test"},
  models: ["resolved-model"], fetchMode: "per_task",
  routes: [{method: "POST", path: "/vendor/jobs", type: "submit", decode: "decodeJob", render: "jobCreated"}],
};
export const native = {
  decodeJob: function() { return {kind: "submit", model: "resolved-model", originTaskIds: ["missing"], requestBody: {}}; },
  jobCreated: function(ctx, task) { return task; },
};
export function buildSubmitRequest() { return {url: "https://example.com"}; }
export function parseSubmitResponse() { return {taskId: "one"}; }
export function buildQueryRequest() { return {url: "https://example.com"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`)
	reached := false
	router := gin.New()
	router.POST("/vendor/jobs", pinTaskPluginRoute(plugin, 0), func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserId, 7)
		c.Next()
	}, PrepareTaskPluginRoute(), func(c *gin.Context) { reached = true })
	request := httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{"model":"resolved-model"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.False(t, reached)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPluginOriginRouteHonorsPriceLimitBeforeSubmission(t *testing.T) {
	require.NoError(t, appI18n.Init())
	plugin := compileTaskRoutePlugin(t, `
export const meta = {
  apiVersion: 1, key: "origin-price", name: "Origin", version: "1.0.0",
  author: {name: "Test"}, models: ["resolved-model"], fetchMode: "per_task",
  routes: [{method: "POST", path: "/vendor/jobs", type: "submit", decode: "decodeJob", render: "jobCreated"}],
};
export const native = {
  decodeJob: function() { return {kind: "submit", model: "resolved-model", originTaskIds: ["task-route"], requestBody: {prompt: "ok"}}; },
  jobCreated: function(ctx, task) { return task; },
};
export function buildSubmitRequest() { return {url: "https://example.com"}; }
export function parseSubmitResponse() { return {taskId: "one"}; }
export function buildQueryRequest() { return {url: "https://example.com"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
`)
	for _, mode := range []string{model.TokenRouteAuto, model.TokenRouteManual} {
		for _, ceiling := range []string{"1", "2"} {
			t.Run(mode+"/limit="+ceiling, func(t *testing.T) {
				setupOriginTaskDB(t)
				previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
				common.MemoryCacheEnabled, common.RedisEnabled = false, false
				t.Cleanup(func() { common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis })
				require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
				a := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
				b := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
				require.NoError(t, model.DB.Model(a).Updates(map[string]any{
					"type": constant.ChannelTypeTaskPlugin, "setting": `{"task_plugin_key":"origin-price"}`, "channel_ratio": "2",
				}).Error)
				insertOriginOwnedTask(t, "task-route", 7, a.Id, "origin-price")
				token := model.Token{UserId: 7, Key: "origin-price-key", Status: common.TokenStatusEnabled}
				require.NoError(t, model.DB.Create(&token).Error)
				var entries []model.TokenRouteEntry
				if mode == model.TokenRouteManual {
					entries = []model.TokenRouteEntry{{ChannelID: b.Id, Enabled: true}}
				}
				_, err := model.SaveTokenRouting(context.Background(), 7, token.Id, model.TokenRouting{
					TokenRouteProfile: model.TokenRouteProfile{Mode: mode, MaxChannelRatio: &ceiling},
					Entries:           entries,
				}, map[int]bool{b.Id: true})
				require.NoError(t, err)

				reached := false
				router := gin.New()
				router.POST("/vendor/jobs", pinTaskPluginRoute(plugin, 0), func(c *gin.Context) {
					common.SetContextKey(c, constant.ContextKeyUserId, 7)
					common.SetContextKey(c, constant.ContextKeyTokenId, token.Id)
					c.Next()
				}, PrepareTaskPluginRoute(), Distribute(), func(c *gin.Context) {
					reached = true
					assert.Equal(t, a.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
					c.Status(http.StatusNoContent)
				})
				request := httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{"model":"resolved-model"}`))
				request.Header.Set("Content-Type", "application/json")
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)

				if ceiling == "1" {
					assert.False(t, reached, "the submission handler cannot pre-consume or send a rejected continuation")
					assert.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
					assert.Contains(t, recorder.Body.String(), `"code":"channel_ratio_exceeded"`)
				} else {
					assert.True(t, reached)
					assert.Equal(t, http.StatusNoContent, recorder.Code, recorder.Body.String())
				}
				assert.Zero(t, service.ChannelInFlight(a.Id))
				assert.Zero(t, service.ChannelInFlight(b.Id))
			})
		}
	}
}

func TestPrepareTaskPluginEndpointPinsOriginTaskChannel(t *testing.T) {
	setupOriginTaskDB(t)
	channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
	insertOriginOwnedTask(t, "task-endpoint", 7, channel.Id, "origin-endpoint")
	const key = "origin-endpoint"
	_, err := jsplugin.DefaultRegistry.Register(taskProtocolPluginSource(
		key,
		"1.0.0",
		`["claimed-model"]`,
		"/v1/responses",
		`return {model: ctx.model, originTaskIds: ["task-endpoint"], requestBody: {prompt: "ok"}};`,
	), jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })

	reached := false
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserId, 7)
		c.Next()
	}, PinTaskPluginEndpoint(), PrepareTaskPluginEndpoint(), func(c *gin.Context) {
		reached = true
		pinnedID, ok := resolvedOriginPin(c)
		require.True(t, ok)
		assert.Equal(t, channel.Id, pinnedID)
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"claimed-model","input":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.True(t, reached)
	assert.Equal(t, http.StatusNoContent, recorder.Code)
}

func TestPrepareTaskPluginEndpointRejectsUnknownOriginTask(t *testing.T) {
	setupOriginTaskDB(t)
	const key = "origin-endpoint-missing"
	_, err := jsplugin.DefaultRegistry.Register(taskProtocolPluginSource(
		key,
		"1.0.0",
		`["claimed-model"]`,
		"/v1/responses",
		`return {model: ctx.model, originTaskIds: ["missing"], requestBody: {prompt: "ok"}};`,
	), jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })

	reached := false
	router := gin.New()
	router.POST("/v1/responses", func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyUserId, 7)
		c.Next()
	}, PinTaskPluginEndpoint(), PrepareTaskPluginEndpoint(), func(c *gin.Context) { reached = true })
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"claimed-model","input":"hello"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	assert.False(t, reached)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "origin_task_not_found")
}

func TestDistributeHonorsOriginTaskChannelPin(t *testing.T) {
	require.NoError(t, appI18n.Init())
	setupOriginTaskDB(t)
	channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	c.Set("resolved_task_model", "resolved-model")
	service.GetChannelConstraints(c).AddPin(dto.ChannelPin{
		ChannelId: channel.Id,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})

	nextCalled := false
	handler := Distribute()
	handler(c)
	if !c.IsAborted() {
		nextCalled = true
	}
	assert.True(t, nextCalled)
	assert.Equal(t, channel.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
}

func TestPluginOriginPinDoesNotBypassTokenModelLimit(t *testing.T) {
	require.NoError(t, appI18n.Init())
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprint("allowed=", allowed), func(t *testing.T) {
			setupOriginTaskDB(t)
			channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{"model":"body-alias"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set("resolved_task_model", "resolved-model")
			common.SetContextKey(c, constant.ContextKeyUserId, 1)
			common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
			limits := map[string]bool{"body-alias": true}
			if allowed {
				limits["resolved-model"] = true
			}
			common.SetContextKey(c, constant.ContextKeyTokenModelLimit, limits)
			service.GetChannelConstraints(c).AddPin(dto.ChannelPin{
				ChannelId: channel.Id, Source: dto.PinSourceOriginTask,
				Rank: dto.PinRankOriginTask, RetryMode: dto.PinRetrySameChannel,
			})
			Distribute()(c)
			if allowed {
				assert.False(t, c.IsAborted())
				assert.Equal(t, channel.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
			} else {
				assert.True(t, c.IsAborted())
				assert.Equal(t, http.StatusForbidden, recorder.Code)
				assert.Zero(t, service.ChannelInFlight(channel.Id))
			}
		})
	}
}

func TestDistributeTokenPinBeatsOriginPin(t *testing.T) {
	require.NoError(t, appI18n.Init())
	setupOriginTaskDB(t)
	tokenChannel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
	originChannel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)

	var warnBuf bytes.Buffer
	previousWriter := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &warnBuf
	t.Cleanup(func() { gin.DefaultErrorWriter = previousWriter })

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	c.Set("resolved_task_model", "resolved-model")
	constraints := service.GetChannelConstraints(c)
	constraints.AddPin(dto.ChannelPin{
		ChannelId: tokenChannel.Id,
		Source:    dto.PinSourceToken,
		Rank:      dto.PinRankToken,
		RetryMode: dto.PinRetrySingleAttempt,
	})
	constraints.AddPin(dto.ChannelPin{
		ChannelId: originChannel.Id,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})
	nextCalled := false
	Distribute()(c)
	if !c.IsAborted() {
		nextCalled = true
	}
	assert.True(t, nextCalled)
	assert.Equal(t, tokenChannel.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
	warn := warnBuf.String()
	assert.Contains(t, warn, "winning_source=token")
	assert.Contains(t, warn, fmt.Sprintf("winning_channel_id=%d", tokenChannel.Id))
	assert.Contains(t, warn, "overridden_source=origin_task")
	assert.Contains(t, warn, fmt.Sprintf("overridden_channel_id=%d", originChannel.Id))
}

func TestDistributePinViolatingIdentityFilterErrors(t *testing.T) {
	require.NoError(t, appI18n.Init())
	setupOriginTaskDB(t)
	channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/vendor/jobs", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 1)
	c.Set("resolved_task_model", "resolved-model")
	c.Set("expected_task_plugin_key", "alpha")
	service.GetChannelConstraints(c).AddPin(dto.ChannelPin{
		ChannelId: channel.Id,
		Source:    dto.PinSourceOriginTask,
		Rank:      dto.PinRankOriginTask,
		RetryMode: dto.PinRetrySameChannel,
	})

	Distribute()(c)
	assert.True(t, c.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), string(dto.FilterTaskPluginIdentity))
}

// Building the origin pin reads the origin channel the same way with the
// memory cache on or off: a channel missing only from the cache is still
// found and a deletion is confirmed by the database. Storage errors retain
// the upstream preparation-stage 400 response.
func TestApplyOriginTaskIntentReadsOriginChannelThroughCache(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprint("cache=", cache), func(t *testing.T) {
			setupOriginTaskDB(t)
			previousCache := common.MemoryCacheEnabled
			common.MemoryCacheEnabled = cache
			t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
			model.InitChannelCache()
			channel := insertOriginTaskChannel(t, common.ChannelStatusEnabled)
			insertOriginOwnedTask(t, "task-own", 7, channel.Id, "origin-plugin")
			meta := jsplugin.Meta{Key: "origin-plugin"}
			intent := map[string]any{"originTaskIds": []any{"task-own"}}

			c := originTaskTestContext(7)
			require.Nil(t, applyOriginTaskIntent(c, intent, meta), "a channel created after the cache was built is read from the database")
			pinnedID, _ := resolvedOriginPin(c)
			require.Equal(t, channel.Id, pinnedID)

			require.NoError(t, model.DB.Delete(&model.Channel{}, channel.Id).Error)
			intentErr := applyOriginTaskIntent(originTaskTestContext(7), intent, meta)
			require.NotNil(t, intentErr)
			assert.Equal(t, "origin_task_channel_disabled", intentErr.Code)

			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("test:origin-intent-channel-storage", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
					tx.AddError(fmt.Errorf("private origin channel storage failure"))
				}
			}))
			intentErr = applyOriginTaskIntent(originTaskTestContext(7), intent, meta)
			require.NotNil(t, intentErr)
			assert.Equal(t, http.StatusBadRequest, intentErr.StatusCode)
			assert.Equal(t, "origin_task_channel_disabled", intentErr.Code)
			assert.NotContains(t, intentErr.Message, "private")
		})
	}
}
