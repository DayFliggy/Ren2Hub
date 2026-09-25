package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	taskjs "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPluginMetadataPreviewNeverExecutesAndReportsDynamicFields(t *testing.T) {
	source := `throw new Error("executed"); export const meta={apiVersion:1,key:"fixture",version:"1.0.0",models:(()=>{throw new Error("executed")})()};`
	body, err := common.Marshal(map[string]any{"source": source})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/plugin/task/preview", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	PreviewTaskPlugin(c)
	assert.Contains(t, w.Body.String(), `"success":true`)
	assert.Contains(t, w.Body.String(), `"unresolved":["models"]`)
}

func TestTaskPluginSyncReusesHashAndSuppressesDisabledFactory(t *testing.T) {
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}, &model.BillingRecovery{}, &model.Ability{}, &model.ChannelCapabilitySnapshot{}, &model.ChannelModelCapability{}))
	previousRegistry := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	previousDisabled := setting.GetTaskPluginDisabledFactoryKeys()
	taskPluginSyncState.Lock()
	previousHashes, previousErrors, previousRebuild := taskPluginSyncState.hashes, taskPluginSyncState.errors, taskPluginSyncState.lastRebuild
	taskPluginSyncState.hashes, taskPluginSyncState.errors = map[string]string{}, map[string]string{}
	taskPluginSyncState.Unlock()
	t.Cleanup(func() {
		jsplugin.DefaultRegistry = previousRegistry
		require.NoError(t, setting.SetTaskPluginDisabledFactoryKeysOption(previousDisabled))
		taskPluginSyncState.Lock()
		taskPluginSyncState.hashes, taskPluginSyncState.errors, taskPluginSyncState.lastRebuild = previousHashes, previousErrors, previousRebuild
		taskPluginSyncState.Unlock()
	})
	factory, err := plugins.Source("sunoapi")
	require.NoError(t, err)
	_, err = jsplugin.DefaultRegistry.RegisterFactory(factory, jsplugin.Options{})
	require.NoError(t, err)
	source := strings.Replace(factory, `version: "1.1.0"`, `version: "2.0.0"`, 1)
	plugin := model.TaskPlugin{Key: "sunoapi", Version: "2.0.0", APIVersion: 1, Enabled: true, Source: model.LongText(source), SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}
	require.NoError(t, model.SaveTaskPlugin(&plugin))
	queries := 0
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("count_plugin_source", func(tx *gorm.DB) {
		if tx.Statement.Table == "task_plugins" && slices.Contains(tx.Statement.Selects, "source") {
			queries++
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Query().Remove("count_plugin_source")) })
	require.NoError(t, syncTaskPluginsOnce())
	assert.Equal(t, 1, queries)
	active, found := jsplugin.DefaultRegistry.Get("sunoapi")
	require.True(t, found)
	assert.Equal(t, "2.0.0", active.Meta.Version)
	require.NoError(t, syncTaskPluginsOnce())
	assert.Equal(t, 1, queries, "unchanged hashes must not load source again")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Params = gin.Params{{Key: "key", Value: "sunoapi"}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/plugin/task/sunoapi/status", strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	SetTaskPluginStatus(c)
	_, found = jsplugin.DefaultRegistry.Get("sunoapi")
	assert.False(t, found, "disabling an override cannot expose the factory layer")
	assert.True(t, setting.IsTaskPluginFactoryDisabled("sunoapi"))
}

func TestPluginArtifactProjectionUsesPinnedVersionAfterActivation(t *testing.T) {
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}, &model.BillingRecovery{}))
	makePlugin := func(version, artifact string) model.TaskPlugin {
		source := fmt.Sprintf(`export const meta={apiVersion:1,key:"artifact-fixture",name:"Fixture",version:%q,author:{name:"Test"},models:["model"],fetchMode:"per_task"};export function buildSubmitRequest(){return {url:"https://example.com"}};export function parseSubmitResponse(){return {taskId:"job"}};export function buildQueryRequest(){return {url:"https://example.com"}};export function parseTaskResult(){return {status:"SUCCESS"}};export function listArtifacts(){return [{key:%q,type:"video"}]};export function buildContentRequest(){return {url:"https://example.com/video",method:"GET",credentialless:true}}`, version, artifact)
		p := model.TaskPlugin{Key: "artifact-fixture", Version: version, APIVersion: 1, Enabled: true, Source: model.LongText(source), SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}
		require.NoError(t, model.SaveTaskPlugin(&p))
		return p
	}
	first := makePlugin("1.0.0", "old-video")
	second := makePlugin("2.0.0", "new-video")
	ref := model.TaskPluginRef{Key: first.Key, Version: first.Version, SourceHash: first.SourceHash}
	_, compileErr := service.LoadTaskPluginExact(ref)
	require.NoError(t, compileErr)
	require.NoError(t, model.PinTaskPluginAttempt(context.Background(), "request", "attempt", ref))
	require.NoError(t, model.ActivateTaskPlugin(second.Key, second.Version))
	task := &model.Task{Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{PluginRef: &ref}}
	items, err := projectTaskArtifacts(task)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "old-video", items[0].Key)
	_, err = model.DeleteTaskPluginVersion(first.Key, first.Version)
	require.ErrorIs(t, err, model.ErrTaskPluginInUse)
	task.PrivateData.ResultDiscarded = true
	items, err = projectTaskArtifacts(task)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestPluginQueryCancellationReachesHTTP(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(cancelled) }))
	defer server.Close()
	source := `export const meta={apiVersion:1,key:"cancel",name:"Cancel",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};export function buildSubmitRequest(ctx){return {url:ctx.baseUrl}};export function parseSubmitResponse(){return {taskId:"job"}};export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/job"}};export function parseTaskResult(){return {status:"IN_PROGRESS"}};`
	plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	service.InitHttpClient()
	adaptor := taskjs.New(plugin)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	adaptor.SetTaskContext(ctx)
	done := make(chan error, 1)
	go func() {
		response, err := adaptor.FetchTaskForTask(server.URL, "", &model.Task{TaskID: "job"}, "")
		if response != nil {
			response.Body.Close()
		}
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query did not start")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("query ignored cancellation")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("upstream ignored cancellation")
	}
}

func TestRetrieveTaskPluginResponseRetainsExactVersionAndTokenOwnership(t *testing.T) {
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{}))
	makePlugin := func(version, text string) model.TaskPlugin {
		source := fmt.Sprintf(`
export const meta={apiVersion:1,key:"retrieve-version-fixture",name:"Fixture",version:%q,author:{name:"Test"},models:["model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync"]}]};
export function buildSubmitRequest(){return {url:"https://example.com"};}
export function parseSubmitResponse(){return {taskId:"job"};}
export function buildQueryRequest(){return {url:"https://example.com"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
export const protocols={openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,requestBody:ctx.body.value};},renderFinal:function(){return {output:[{type:"message",status:"completed",role:"assistant",content:[{type:"output_text",text:%q,annotations:[],logprobs:[]}]}]};}}};`, version, text)
		plugin := model.TaskPlugin{Key: "retrieve-version-fixture", Version: version, APIVersion: 1, Enabled: true, Source: model.LongText(source), SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}
		require.NoError(t, model.SaveTaskPlugin(&plugin))
		return plugin
	}
	first := makePlugin("1.0.0", "original-result")
	second := makePlugin("2.0.0", "new-result")
	ref := model.TaskPluginRef{Key: first.Key, Version: first.Version, SourceHash: first.SourceHash}
	require.NoError(t, model.PinTaskPluginAttempt(context.Background(), "retrieve-request", "retrieve-attempt", ref))
	task := &model.Task{TaskID: "task_pinned_version", UserId: 71, Status: model.TaskStatusSuccess,
		Properties:  model.Properties{OriginModelName: "model"},
		PrivateData: model.TaskPrivateData{TokenId: 81, PluginRef: &ref, PluginAttemptID: "retrieve-attempt", SubmissionRequestID: "retrieve-request"}}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		return model.AttachTaskPluginExecution(tx, task)
	}))
	require.NoError(t, model.ActivateTaskPlugin(second.Key, second.Version))

	c, recorder := newPluginProtocolRetrieveContext("resp_pinned_version")
	retrieveTaskPluginResponse(c, pluginProtocolBridgeDeps{})
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), "original-result")
	assert.NotContains(t, recorder.Body.String(), "new-result")

	c, recorder = newPluginProtocolRetrieveContext("resp_pinned_version")
	c.Set("token_id", 82)
	retrieveTaskPluginResponse(c, pluginProtocolBridgeDeps{})
	assert.Equal(t, http.StatusNotFound, recorder.Code)
	assert.NotContains(t, recorder.Body.String(), "original-result")

	task.PrivateData.PluginRef = &model.TaskPluginRef{Key: first.Key, Version: "missing", SourceHash: first.SourceHash}
	require.NoError(t, db.Model(task).Update("private_data", task.PrivateData).Error)
	c, recorder = newPluginProtocolRetrieveContext("resp_pinned_version")
	retrieveTaskPluginResponse(c, pluginProtocolBridgeDeps{})
	assert.Equal(t, http.StatusNotFound, recorder.Code, "a missing pinned version must not select the active version")
	assert.NotContains(t, recorder.Body.String(), "new-result")
}
