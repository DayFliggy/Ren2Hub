package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const disableFixtureKey = "disable-fixture"

type pluginDisableResponse struct {
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	MessageKey string `json:"message_key"`
	Data       struct {
		Channels         []model.TaskPluginChannelRef `json:"channels"`
		InFlightCount    int64                        `json:"in_flight_count"`
		PluginEnabled    bool                         `json:"plugin_enabled"`
		DisabledChannels int                          `json:"disabled_channels"`
		UnboundChannels  int                          `json:"unbound_channels"`
	} `json:"data"`
}

func setupPluginDisableAPI(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	db, router := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{},
		&model.TaskPluginExecution{}, &model.BillingRecovery{}, &model.Ability{},
		&model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}))
	previousRegistry := jsplugin.DefaultRegistry
	previousDisabled := setting.GetTaskPluginDisabledFactoryKeys()
	previousEnabled, previousMemoryCache := constant.TaskPluginEnabled, common.MemoryCacheEnabled
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	constant.TaskPluginEnabled, common.MemoryCacheEnabled = true, false
	taskPluginSyncState.Lock()
	previousHashes, previousErrors, previousRebuild := taskPluginSyncState.hashes, taskPluginSyncState.errors, taskPluginSyncState.lastRebuild
	taskPluginSyncState.hashes, taskPluginSyncState.errors = map[string]string{}, map[string]string{}
	taskPluginSyncState.lastRebuild = taskPluginRebuildOutcome{}
	taskPluginSyncState.Unlock()
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		constant.TaskPluginEnabled, common.MemoryCacheEnabled = previousEnabled, previousMemoryCache
		require.NoError(t, setting.SetTaskPluginDisabledFactoryKeysOption(previousDisabled))
		taskPluginSyncState.Lock()
		taskPluginSyncState.hashes, taskPluginSyncState.errors, taskPluginSyncState.lastRebuild = previousHashes, previousErrors, previousRebuild
		taskPluginSyncState.Unlock()
	})
	require.NoError(t, setting.SetTaskPluginDisabledFactoryKeysOption(nil))
	router.PUT("/api/plugin/task/:key/status", SetTaskPluginStatus)
	router.DELETE("/api/plugin/task/:key/versions/:version", DeleteTaskPluginVersion)
	return db, router
}

func saveDisableFixturePlugin(t *testing.T, key, version string) model.TaskPlugin {
	t.Helper()
	source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:"Disable fixture",version:%q,author:{name:"Test"},models:[%q],fetchMode:"per_task"};
export function buildSubmitRequest(){return {url:"https://fixture.invalid/submit"}};
export function parseSubmitResponse(){return {taskId:"job"}};
export function buildQueryRequest(){return {url:%q}};
export function parseTaskResult(){return {status:"IN_PROGRESS"}};`, key, version, key+"-model", "https://fixture.invalid/query/"+version)
	plugin := model.TaskPlugin{Key: key, Version: version, APIVersion: 1, Enabled: true,
		Source: model.LongText(source), SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}
	require.NoError(t, model.SaveTaskPlugin(&plugin))
	return plugin
}

func seedPluginDisableChannels(t *testing.T, db *gorm.DB) []model.Channel {
	t.Helper()
	baseURL := "https://fixture.invalid"
	channels := []model.Channel{
		{Name: "dedicated", Type: constant.ChannelTypeTaskPlugin, Models: disableFixtureKey + "-model"},
		{Name: "shared", Type: constant.ChannelTypeNewAPI, Models: disableFixtureKey + "-model,other-fixture-model,gpt-4o-mini"},
		{Name: "unrelated", Type: constant.ChannelTypeTaskPlugin, Models: "other-fixture-model"},
	}
	channels[0].SetSetting(dto.ChannelSettings{TaskPluginKey: disableFixtureKey})
	channels[1].SetSetting(dto.ChannelSettings{
		TaskPluginKey: disableFixtureKey, TaskExtendPluginKeys: []string{disableFixtureKey, "other-fixture"},
		SystemPrompt: "keep this prompt", ResponsesWebSocketEnabled: true,
	})
	channels[2].SetSetting(dto.ChannelSettings{TaskPluginKey: "other-fixture"})
	for i := range channels {
		channels[i].BaseURL, channels[i].Status, channels[i].Group = &baseURL, common.ChannelStatusEnabled, "team"
		channels[i].Key = "fixture-only-key"
		require.NoError(t, db.Create(&channels[i]).Error)
		require.NoError(t, channels[i].AddAbilities(db))
	}
	return channels
}

func requestPluginDisable(t *testing.T, router *gin.Engine, query string) pluginDisableResponse {
	t.Helper()
	response := gatewayAPIRequest(t, router, http.MethodPut, "/api/plugin/task/"+disableFixtureKey+"/status"+query, map[string]bool{"enabled": false})
	require.Equal(t, http.StatusOK, response.Code)
	var result pluginDisableResponse
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	return result
}

func TestTaskPluginDisableRequiresCascadeForBoundChannels(t *testing.T) {
	db, router := setupPluginDisableAPI(t)
	saveDisableFixturePlugin(t, disableFixtureKey, "1.0.0")
	saveDisableFixturePlugin(t, "other-fixture", "1.0.0")
	channels := seedPluginDisableChannels(t, db)
	require.NoError(t, syncTaskPluginsOnce())

	for _, query := range []string{"", "?force=true"} {
		result := requestPluginDisable(t, router, query)
		assert.False(t, result.Success, "force does not replace cascade consent")
		assert.Equal(t, "Task plugin is still in use", result.MessageKey)
		assert.ElementsMatch(t, []model.TaskPluginChannelRef{
			{Id: channels[0].Id, Name: channels[0].Name, Type: channels[0].Type},
			{Id: channels[1].Id, Name: channels[1].Name, Type: channels[1].Type},
		}, result.Data.Channels)
		assert.Zero(t, result.Data.InFlightCount)
	}
	for _, before := range channels {
		var after model.Channel
		require.NoError(t, db.First(&after, before.Id).Error)
		assert.Equal(t, before.Status, after.Status)
		assert.Equal(t, before.Setting, after.Setting)
	}
	active, err := model.GetTaskPluginVersion(disableFixtureKey, "")
	require.NoError(t, err)
	assert.True(t, active.Enabled)
	_, available := jsplugin.DefaultRegistry.Get(disableFixtureKey)
	assert.True(t, available)
}

func TestTaskPluginDisableCascadePreservesOtherNewAPIBindings(t *testing.T) {
	db, router := setupPluginDisableAPI(t)
	saveDisableFixturePlugin(t, disableFixtureKey, "1.0.0")
	saveDisableFixturePlugin(t, "other-fixture", "1.0.0")
	channels := seedPluginDisableChannels(t, db)
	require.NoError(t, syncTaskPluginsOnce())

	result := requestPluginDisable(t, router, "?cascade=true")
	require.True(t, result.Success, result.Message)
	assert.False(t, result.Data.PluginEnabled)
	assert.Equal(t, 1, result.Data.DisabledChannels)
	assert.Equal(t, 1, result.Data.UnboundChannels)
	var dedicated, shared, unrelated model.Channel
	require.NoError(t, db.First(&dedicated, channels[0].Id).Error)
	require.NoError(t, db.First(&shared, channels[1].Id).Error)
	require.NoError(t, db.First(&unrelated, channels[2].Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, dedicated.Status)
	assert.Equal(t, channels[0].Setting, dedicated.Setting)
	assert.Equal(t, common.ChannelStatusEnabled, shared.Status)
	assert.Equal(t, channels[1].Models, shared.Models)
	wantSettings := channels[1].GetSetting()
	wantSettings.TaskPluginKey = ""
	wantSettings.TaskExtendPluginKeys = []string{"other-fixture"}
	assert.Equal(t, wantSettings, shared.GetSetting())
	assert.Equal(t, channels[2].Status, unrelated.Status)
	assert.Equal(t, channels[2].Setting, unrelated.Setting)
	var abilities []model.Ability
	require.NoError(t, db.Find(&abilities).Error)
	require.Len(t, abilities, 5)
	for _, ability := range abilities {
		assert.Equal(t, ability.ChannelId != dedicated.Id, ability.Enabled, "channel %d model %s", ability.ChannelId, ability.Model)
	}
	active, err := model.GetTaskPluginVersion(disableFixtureKey, "")
	require.NoError(t, err)
	assert.False(t, active.Enabled)
	_, available := jsplugin.DefaultRegistry.Get(disableFixtureKey)
	assert.False(t, available)
	_, available = jsplugin.DefaultRegistry.Get("other-fixture")
	assert.True(t, available)
}

func TestTaskPluginForceDisableRetainsPinnedExecutionAndBlocksDeletion(t *testing.T) {
	db, router := setupPluginDisableAPI(t)
	oldVersion := saveDisableFixturePlugin(t, disableFixtureKey, "1.0.0")
	newVersion := saveDisableFixturePlugin(t, disableFixtureKey, "2.0.0")
	saveDisableFixturePlugin(t, "other-fixture", "1.0.0")
	channels := seedPluginDisableChannels(t, db)
	ref := model.TaskPluginRef{Key: oldVersion.Key, Version: oldVersion.Version, SourceHash: oldVersion.SourceHash}
	require.NoError(t, model.PinTaskPluginAttempt(context.Background(), "disable-request", "disable-attempt", ref))
	task := model.Task{TaskID: "disable-task", Platform: constant.TaskPlatform(disableFixtureKey),
		UserId: 1, ChannelId: channels[0].Id, Status: model.TaskStatusInProgress, Progress: "42%", Quota: 17,
		PrivateData: model.TaskPrivateData{PluginRef: &ref, PluginAttemptID: "disable-attempt",
			SubmissionRequestID: "disable-request", UpstreamTaskID: "old-upstream-job"}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&task).Error; err != nil {
			return err
		}
		return model.AttachTaskPluginExecution(tx, &task)
	}))
	require.NoError(t, model.ActivateTaskPlugin(newVersion.Key, newVersion.Version))
	require.NoError(t, syncTaskPluginsOnce())
	var beforeTask model.Task
	var beforeExecution model.TaskPluginExecution
	require.NoError(t, db.First(&beforeTask, task.ID).Error)
	require.NoError(t, db.First(&beforeExecution, "attempt_id = ?", "disable-attempt").Error)
	currentRef := model.TaskPluginRef{Key: newVersion.Key, Version: newVersion.Version, SourceHash: newVersion.SourceHash}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return model.ValidateTaskPluginAdmissionWithTx(tx, currentRef) }))

	blocked := requestPluginDisable(t, router, "?cascade=true")
	assert.False(t, blocked.Success)
	assert.Equal(t, "Task plugin is still in use", blocked.MessageKey)
	assert.EqualValues(t, 1, blocked.Data.InFlightCount)
	require.Len(t, blocked.Data.Channels, 2)
	for _, before := range channels {
		var after model.Channel
		require.NoError(t, db.First(&after, before.Id).Error)
		assert.Equal(t, before.Status, after.Status, "rejected force confirmation cannot partly cascade")
		assert.Equal(t, before.Setting, after.Setting)
	}
	active, err := model.GetTaskPluginVersion(disableFixtureKey, "")
	require.NoError(t, err)
	assert.True(t, active.Enabled)

	forced := requestPluginDisable(t, router, "?cascade=true&force=true")
	require.True(t, forced.Success, forced.Message)
	assert.False(t, forced.Data.PluginEnabled)
	assert.Equal(t, 1, forced.Data.DisabledChannels)
	assert.Equal(t, 1, forced.Data.UnboundChannels)
	require.ErrorContains(t, db.Transaction(func(tx *gorm.DB) error {
		return model.ValidateTaskPluginAdmissionWithTx(tx, currentRef)
	}), "plugin publication changed before sending")
	_, available := jsplugin.DefaultRegistry.Get(disableFixtureKey)
	assert.False(t, available)
	var afterTask model.Task
	var afterExecution model.TaskPluginExecution
	require.NoError(t, db.First(&afterTask, task.ID).Error)
	require.NoError(t, db.First(&afterExecution, "attempt_id = ?", "disable-attempt").Error)
	assert.Equal(t, beforeTask, afterTask, "force stops new admission without rewriting existing work")
	assert.Equal(t, beforeExecution, afterExecution)
	retained, err := model.GetTaskPluginExact(ref)
	require.NoError(t, err)
	assert.False(t, retained.Active, "deletion below must exercise retained execution protection, not active-version protection")
	assert.Equal(t, oldVersion.Source, retained.Source)
	loaded, err := service.LoadTaskPluginForTask(&afterTask)
	require.NoError(t, err)
	assert.Equal(t, oldVersion.SourceHash, loaded.SourceHash)
	query, err := loaded.Engine.Call(context.Background(), "buildQueryRequest")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"url": "https://fixture.invalid/query/1.0.0"}, query)

	response := gatewayAPIRequest(t, router, http.MethodDelete,
		"/api/plugin/task/"+disableFixtureKey+"/versions/1.0.0?force=true", nil)
	require.Equal(t, http.StatusOK, response.Code)
	var deletion pluginDisableResponse
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &deletion))
	assert.False(t, deletion.Success)
	assert.Equal(t, model.ErrTaskPluginInUse.Error(), deletion.Message)
	_, err = model.GetTaskPluginExact(ref)
	require.NoError(t, err)
	require.NoError(t, db.First(&afterExecution, "attempt_id = ?", "disable-attempt").Error)
	assert.Equal(t, beforeExecution, afterExecution)
	require.NoError(t, db.First(&afterTask, task.ID).Error)
	assert.Equal(t, beforeTask, afterTask)
}
