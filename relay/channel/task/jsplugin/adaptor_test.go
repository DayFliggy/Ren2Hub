package jsplugin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mockPlugin = `
export const meta = {
  apiVersion: 1, key: "mock-task", name: "Mock Task", version: "1.0.0",
  author: {name: "Test"},
  channelTypes: [1001], models: ["mock-v1"], fetchMode: "per_task",
  protocols: ["openai_video"],
  usageSchema: {seconds: {type: "number", unit: "second"}, mode: {enum: ["std", "pro"]}, upstreamUnits:{type:"number",unit:"credit"}},
};
export function buildSubmitRequest(ctx) {
  if (!ctx.requestBody.prompt) throw new Error("prompt required");
  return { url: ctx.baseUrl + "/submit", method: "POST", headers: {"X-Plugin": "submit"}, body: {prompt: ctx.requestBody.prompt}, action: "text_to_video", model: "mock-v1", rewriteModel: "mock-upstream" };
}

export function parseSubmitResponse(ctx, resp) {
  return {
    taskId: resp.body.id,
    taskData: {accepted: true, status: resp.statusCode},
  };
}
export function extractUsage(ctx) { return {seconds: 5, mode: "pro"}; }
export function extractUsageOnSubmit(ctx, data) { return {seconds: data.seconds || 7}; }
export function extractUsageOnComplete(task, result) { return {upstreamUnits: 23}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/tasks/" + ctx.taskId, method: "GET", headers: {"X-Plugin": "query"}}; }
export function parseTaskResult(ctx, body) { return {taskId: body.id, status: "SUCCESS", progress: "100%", url: body.url}; }
export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }
export const protocols = {openai_video: {
  decodeRequest: function(ctx) { return {kind: "submit", model: ctx.model, requestBody: ctx.body.value}; },
  render: function(ctx, task) { return {id: task.task_id, status: "completed"}; }
}};
`

func TestTaskAdaptorRejectsDeprecatedClientResponse(t *testing.T) {
	source := strings.Replace(mockPlugin, `taskData: {accepted: true, status: resp.statusCode},`, `taskData: {}, clientResponse: {id: ctx.publicTaskId},`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	response := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"upstream"}`))}

	parsed, taskErr := adaptor.ParseResponse(c, response, info)

	assert.Nil(t, parsed)
	require.NotNil(t, taskErr)
	require.Error(t, taskErr.Error)
	assert.Contains(t, taskErr.Error.Error(), "must not return clientResponse")
}

func TestTaskAdaptorBuildsMultipartFromOpaqueFileReference(t *testing.T) {
	// The plugin forwards the client's Content-Type, whose boundary is not the one of the rebuilt body.
	source := `
export const meta = {apiVersion:1,key:"multipart",name:"Multipart",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url:ctx.baseUrl+"/submit",headers:{"Content-Type":ctx.requestHeaders["Content-Type"]},bodyType:"multipart",parts:[{name:"model",value:"m"},{name:"input_reference",fileRef:ctx.files[0].ref}]}; }
export function parseSubmitResponse(ctx,r){return {taskId:"1"}} export function buildQueryRequest(){return {url:"https://example.com"}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	file, err := writer.CreateFormFile("input_reference", "ref.png")
	require.NoError(t, err)
	_, err = file.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "p"})
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	requestBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	upstreamRequest := httptest.NewRequest(http.MethodPost, "https://provider.example/submit", bytes.NewReader(requestBytes))
	require.NoError(t, adaptor.BuildRequestHeader(c, upstreamRequest, info))
	mediaType, params, err := mime.ParseMediaType(upstreamRequest.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/form-data", mediaType)
	require.NotEmpty(t, params["boundary"])
	reader := multipart.NewReader(bytes.NewReader(requestBytes), params["boundary"])
	form, err := reader.ReadForm(1024)
	require.NoError(t, err)
	assert.Equal(t, []string{"m"}, form.Value["model"])
	require.Len(t, form.File["input_reference"], 1)
	opened, err := form.File["input_reference"][0].Open()
	require.NoError(t, err)
	content, err := io.ReadAll(opened)
	require.NoError(t, err)
	assert.Equal(t, "image-bytes", string(content))
}

func TestTaskAdaptorInlinesJSONFilePlaceholders(t *testing.T) {
	const fileBytes = "image-bytes"
	encoded := base64.StdEncoding.EncodeToString([]byte(fileBytes))
	source := `
export const meta = {apiVersion:1,key:"json-inline",name:"JSON Inline",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url:ctx.baseUrl+"/submit",body:{
    prompt:"p",
    image:{__fileRef:ctx.files[0].ref,encoding:"base64"},
    nested:{items:[{__fileRef:ctx.files[0].ref,encoding:"dataUrl",mimeType:"image/png"}]},
    dataUrl:{__fileRef:ctx.files[0].ref,encoding:"dataUrl"},
    refs:ctx.files.map(function(file){return file.ref;})
  }};
}
export function parseSubmitResponse(){return {taskId:"1"}} export function buildQueryRequest(){return {url:"https://example.com"}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	c := newMultipartFileContext(t, "input_reference", "ref.png", "image/jpeg", []byte(fileBytes))
	c.Set("task_request", map[string]any{"prompt": "p"})
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	requestBytes, err := io.ReadAll(body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(requestBytes, &decoded))
	assert.Equal(t, "p", decoded["prompt"])
	assert.Equal(t, encoded, decoded["image"])
	nested := decoded["nested"].(map[string]any)
	items := nested["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "data:image/png;base64,"+encoded, items[0])
	assert.Equal(t, "data:image/jpeg;base64,"+encoded, decoded["dataUrl"])
	assert.Equal(t, []any{"request_file:input_reference"}, decoded["refs"])

	t.Run("repeated field files are addressed by index", func(t *testing.T) {
		second := base64.StdEncoding.EncodeToString([]byte("second-bytes"))
		source := `
export const meta = {apiVersion:1,key:"json-inline-repeat",name:"JSON Inline Repeat",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url:ctx.baseUrl+"/submit",body:{
    refs:ctx.files.map(function(file){return file.ref;}),
    images:ctx.files.map(function(file){return {__fileRef:file.ref,encoding:"base64"};}),
    missing:{__fileRef:"request_file:image[]#2",encoding:"base64"}
  }};
}
export function parseSubmitResponse(){return {taskId:"1"}} export function buildQueryRequest(){return {url:"https://example.com"}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
		plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
		require.NoError(t, err)
		adaptor := New(plugin)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
		adaptor.Init(info)
		var input bytes.Buffer
		writer := multipart.NewWriter(&input)
		for _, content := range []string{fileBytes, "second-bytes"} {
			file, createErr := writer.CreateFormFile("image[]", "ref.png")
			require.NoError(t, createErr)
			_, err = file.Write([]byte(content))
			require.NoError(t, err)
		}
		require.NoError(t, writer.Close())
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(input.Bytes()))
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		c.Set("task_request", map[string]any{"prompt": "p"})
		_, err = adaptor.BuildRequestBody(c, info)
		require.ErrorContains(t, err, `unknown file reference "request_file:image[]#2"`, "an index beyond the uploaded files is rejected")

		source = strings.Replace(source, `,
    missing:{__fileRef:"request_file:image[]#2",encoding:"base64"}`, "", 1)
		plugin, err = pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
		require.NoError(t, err)
		adaptor = New(plugin)
		adaptor.Init(info)
		body, err := adaptor.BuildRequestBody(c, info)
		require.NoError(t, err)
		requestBytes, err := io.ReadAll(body)
		require.NoError(t, err)
		var decoded map[string]any
		require.NoError(t, common.Unmarshal(requestBytes, &decoded))
		assert.Equal(t, []any{"request_file:image[]", "request_file:image[]#1"}, decoded["refs"])
		assert.Equal(t, []any{encoded, second}, decoded["images"])
	})
}

func TestTaskAdaptorJSONFilePlaceholderErrors(t *testing.T) {
	tests := []struct {
		name        string
		part        string
		fileSize    int
		globalMB    int
		wantContain string
	}{
		{name: "unknown ref", part: `{__fileRef:"request_file:missing",encoding:"base64"}`, fileSize: 4, wantContain: `unknown file reference "request_file:missing"`},
		{name: "extra key", part: `{__fileRef:"request_file:input_reference",encoding:"base64",extra:true}`, fileSize: 4, wantContain: "invalid file placeholder"},
		{name: "missing encoding", part: `{__fileRef:"request_file:input_reference"}`, fileSize: 4, wantContain: "encoding"},
		{name: "oversize maxBytes", part: `{__fileRef:"request_file:input_reference",encoding:"base64",maxBytes:3}`, fileSize: 4, wantContain: "3 byte limit"},
		{name: "oversize global", part: `{__fileRef:"request_file:input_reference",encoding:"base64"}`, fileSize: 2 << 20, globalMB: 1, wantContain: "1048576 byte limit"},
		{name: "multiple references cap", part: `{a:{__fileRef:"request_file:input_reference",encoding:"base64"},b:{__fileRef:"request_file:input_reference",encoding:"base64"}}`, fileSize: 700 << 10, globalMB: 1, wantContain: "1048576 byte limit"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.globalMB > 0 {
				previous := constant.MaxFileDownloadMB
				constant.MaxFileDownloadMB = testCase.globalMB
				t.Cleanup(func() { constant.MaxFileDownloadMB = previous })
			}
			source := strings.Replace(`
export const meta = {apiVersion:1,key:"json-inline-err",name:"JSON Inline Err",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest() { return {url:"https://provider.example/submit",body:PLACEHOLDER}; }
export function parseSubmitResponse(){return {taskId:"1"}} export function buildQueryRequest(){return {url:"https://example.com"}} export function parseTaskResult(){return {status:"SUCCESS"}}
`, "PLACEHOLDER", testCase.part, 1)
			plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			adaptor := New(plugin)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			adaptor.Init(info)
			c := newMultipartFileContext(t, "input_reference", "ref.bin", "application/octet-stream", bytes.Repeat([]byte("x"), testCase.fileSize))
			c.Set("task_request", map[string]any{"prompt": "p"})
			_, err = adaptor.BuildRequestBody(c, info)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantContain)
		})
	}
}

func newMultipartFileContext(t *testing.T, field, filename, contentType string, content []byte) *gin.Context {
	t.Helper()
	var input bytes.Buffer
	writer := multipart.NewWriter(&input)
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="` + field + `"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	return c
}

func TestTaskAdaptorDoesNotEmitInjectedMultipartDispositionHeaders(t *testing.T) {
	tests := []struct {
		name string
		part string
	}{
		{name: "part name", part: `{name:"prompt\r\nX-Injected: yes",value:"hello"}`},
		{name: "filename", part: `{name:"input_reference",fileRef:"request_file:input_reference",filename:"safe.png\r\nX-Injected: yes"}`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			source := strings.Replace(`
export const meta = {apiVersion:1,key:"multipart-safe",name:"Multipart Safe",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url:ctx.baseUrl+"/submit",bodyType:"multipart",parts:[PART]}; }
export function parseSubmitResponse(){return {taskId:"1"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`, "PART", testCase.part, 1)
			plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			adaptor := New(plugin)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			adaptor.Init(info)
			var input bytes.Buffer
			writer := multipart.NewWriter(&input)
			file, err := writer.CreateFormFile("input_reference", "input.png")
			require.NoError(t, err)
			_, err = file.Write([]byte("image"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(input.Bytes()))
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			c.Set("task_request", map[string]any{"model": "m"})

			body, buildErr := adaptor.BuildRequestBody(c, info)
			if buildErr != nil {
				assert.Contains(t, buildErr.Error(), "multipart")
				return
			}
			encoded, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "\r\nX-Injected: yes")
		})
	}
}

func TestTaskAdaptorRejectsPostDistributionEndpointModelDrift(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"endpoint-drift",name:"Endpoint Drift",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url:ctx.baseUrl+"/submit",method:"POST",model:"outside-model",rewriteModel:"allowed-upstream-rewrite"};
}

export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(){return {url:"https://example.com"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "claimed-model",
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("task_request", map[string]any{"model": "claimed-model"})
	c.Set("resolved_task_model", "claimed-model")
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{
		Plugin: plugin,
	})

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, "claimed-model", info.OriginModelName)
	assert.Contains(t, taskErr.Message, "does not match")
}

func TestTaskAdaptorReDecodesFinalCandidateAndRejectsModelDrift(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"redecode",name:"Redecode",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
let calls = 0;
export const protocols = {openai_responses:{decodeRequest:function(ctx){calls++;return {kind:"submit",model:calls === 1 ? ctx.model : "drifted-model",requestBody:ctx.body.value};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}} export function parseSubmitResponse(){return {taskId:"one"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	protocolContext := pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{Body: map[string]any{"kind": "json", "value": map[string]any{"model": "claimed-model"}}, RequestBody: map[string]any{"model": "claimed-model"}},
		Protocol:            "openai_responses", Model: "claimed-model",
	}
	_, err = plugin.Engine.CallPath(context.Background(), "protocols", []string{"openai_responses", "decodeRequest"}, protocolContext.JSValue())
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model"})
	c.Set(pluginruntime.ContextKeyProtocolRequest, protocolContext)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}, OriginModelName: "claimed-model"}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "pinned model")
}

func TestTaskAdaptorRejectsRendererFromFinalProtocolDecoder(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"renderer-reject",name:"Renderer Reject",version:"1.0.0",author:{name:"Test"},models:["claimed-model"],fetchMode:"per_task",protocols:[{name:"openai_responses",supports:["sync","background"]}]};
export const protocols = {openai_responses:{decodeRequest:function(ctx){return {kind:"submit",model:ctx.model,requestBody:ctx.body.value,renderer:"legacy"};},renderFinal:function(){return {};}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}} export function parseSubmitResponse(){return {taskId:"one"}} export function buildQueryRequest(){return {}} export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	protocolContext := pluginruntime.ProtocolRequestContext{
		RouteRequestContext: pluginruntime.RouteRequestContext{Body: map[string]any{"kind": "json", "value": map[string]any{"model": "claimed-model"}}},
		Protocol:            "openai_responses", Model: "claimed-model",
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(pluginruntime.ContextKeyPinnedEndpoint, pluginruntime.PinnedEndpoint{Plugin: plugin, Protocol: "openai_responses", Model: "claimed-model"})
	c.Set(pluginruntime.ContextKeyProtocolRequest, protocolContext)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}, OriginModelName: "claimed-model"}
	adaptor := New(plugin)
	adaptor.Init(info)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.Contains(t, taskErr.Message, "must not return renderer")
}

func TestTaskAdaptorBuildContentRequestHookAndMissingFallback(t *testing.T) {
	source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts(task) { return [{key: "video", type: "video", mimeType: "video/mp4"}]; }
export function buildContentRequest(ctx) {
  if (ctx.data.id !== "raw-upstream" || ctx.upstreamTaskId !== "upstream-task" || ctx.producerVersion !== "0.9.0") throw new Error("bad task context");
  return {url: ctx.baseUrl + "/content/" + ctx.artifactKey, method: ctx.clientRequest.method, headers: {"X-Content": "plugin"}};
}`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "key"}})
	taskData, err := common.Marshal(map[string]any{"id": "raw-upstream"})
	require.NoError(t, err)
	task := &model.Task{
		TaskID: "task-public", Status: model.TaskStatusSuccess, Data: taskData,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-task",
			Execution: &model.TaskExecutionSnapshot{TaskPlugin: &model.TaskPluginSnapshot{
				Version: "0.9.0",
			}},
		},
	}
	artifacts, err := adaptor.ListArtifacts(task)
	require.NoError(t, err)
	require.Equal(t, []channel.TaskArtifact{{Key: "video", Type: "video", MimeType: "video/mp4"}}, artifacts)
	descriptor, err := adaptor.BuildContentRequest(t.Context(), task, "video", channel.TaskArtifactClientRequest{Method: http.MethodHead})
	require.NoError(t, err)
	require.NotNil(t, descriptor)
	assert.Equal(t, "https://provider.example/content/video", descriptor.URL)
	assert.Equal(t, http.MethodHead, descriptor.Method)
	assert.Equal(t, "plugin", descriptor.Headers["X-Content"])

	withoutHook, err := pluginruntime.NewRegistry().Register(`
export const meta = {apiVersion:1,key:"no-artifacts",name:"No Artifacts",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(){return {url:"https://provider.example"};}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`, pluginruntime.Options{})
	require.NoError(t, err)
	fallback := New(withoutHook)
	fallback.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}})
	artifacts, err = fallback.ListArtifacts(&model.Task{})
	require.NoError(t, err)
	assert.Nil(t, artifacts)
	descriptor, err = fallback.BuildContentRequest(t.Context(), &model.Task{}, "video", channel.TaskArtifactClientRequest{Method: http.MethodGet})
	require.NoError(t, err)
	assert.Nil(t, descriptor)
}

func TestTaskAdaptorRejectsInvalidArtifactProjection(t *testing.T) {
	testCases := []struct {
		name       string
		projection string
	}{
		{name: "duplicate key", projection: `[{key:"video",type:"video"},{key:"video",type:"video"}]`},
		{name: "array index identity", projection: `[{key:"video",type:"video",index:0}]`},
		{name: "upstream url", projection: `[{key:"video",type:"video",url:"https://cdn.example/video.mp4"}]`},
		{name: "invalid key", projection: `[{key:"video/0",type:"video"}]`},
		{name: "unsupported type", projection: `[{key:"video",type:"text"}]`},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts() { return `+testCase.projection+`; }
export function buildContentRequest(ctx) { return {url:ctx.baseUrl+"/content",method:"GET"}; }
`, 1)
			plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			adaptor := New(plugin)
			_, err = adaptor.ListArtifacts(&model.Task{TaskID: "task", Status: model.TaskStatusSuccess, Data: []byte(`{}`)})
			require.Error(t, err)
		})
	}
}

func TestTaskAdaptorAllowsExplicitCredentiallessCDNRequest(t *testing.T) {
	source := strings.Replace(mockPlugin, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts() { return [{key:"video",type:"video"}]; }
export function buildContentRequest(ctx) { return {url:"https://cdn.example/video.mp4",method:ctx.clientRequest.method,credentialless:true}; }
`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}})
	descriptor, err := adaptor.BuildContentRequest(
		t.Context(),
		&model.Task{TaskID: "task", Data: []byte(`{}`)},
		"video",
		channel.TaskArtifactClientRequest{Method: http.MethodGet},
	)
	require.NoError(t, err)
	require.NotNil(t, descriptor)
	assert.True(t, descriptor.Credentialless)
	assert.Equal(t, "https://cdn.example/video.mp4", descriptor.URL)
}

func TestTaskAdaptorHookFetchReachesRequestHostsOnly(t *testing.T) {
	service.InitHttpClient()
	extra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"from":"extra"}`))
	}))
	defer extra.Close()
	base := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/info":
			assert.Equal(t, "Bearer key", r.Header.Get("Authorization"))
			_, _ = w.Write([]byte(`{"from":"base"}`))
		case "/moved":
			http.Redirect(w, r, extra.URL+"/elsewhere", http.StatusFound)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	}))
	defer base.Close()
	source := strings.Replace(mockPlugin, `fetchMode: "per_task",`, `fetchMode: "per_task", allowedHosts: ["`+strings.TrimPrefix(extra.URL, "http://")+`"],`, 1)
	source = strings.Replace(source, `export function listArtifacts() { return []; }
export function buildContentRequest() { throw new Error("artifact_not_found"); }`, `export function listArtifacts() { return [{key: "video", type: "video"}]; }
export function buildContentRequest(ctx) {
  const base = utils.fetch({url: ctx.baseUrl + "/info", headers: {Authorization: "Bearer " + ctx.apiKey}});
  const extra = utils.fetch({url: "http://" + meta.allowedHosts[0] + "/info"});
  const moved = utils.fetch({url: ctx.baseUrl + "/moved"});
  let denied = "";
  try { utils.fetch({url: "https://other.example/info"}); } catch (e) { denied = e.message; }
  return {url: ctx.baseUrl + "/content", method: "GET", headers: {
    "X-Base": base.body.from, "X-Extra": extra.body.from, "X-Moved": moved.status + " " + moved.headers.Location, "X-Denied": denied,
  }};
}`, 1)
	source = strings.Replace(source, `render: function(ctx, task) { return {id: task.task_id, status: "completed"}; }`,
		`render: function(ctx, task) { return utils.fetch({url: "https://provider.example/info"}).body; }`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: base.URL, ApiKey: "key"}})

	descriptor, err := adaptor.BuildContentRequest(t.Context(), &model.Task{TaskID: "task", Data: []byte(`{}`)}, "video", channel.TaskArtifactClientRequest{Method: http.MethodGet})
	require.NoError(t, err)
	require.NotNil(t, descriptor)
	assert.Equal(t, map[string]string{
		"X-Base":   "base",
		"X-Extra":  "extra",
		"X-Moved":  "302 " + extra.URL + "/elsewhere",
		"X-Denied": `utils.fetch to other.example failed: plugin request host "other.example" is not allowed`,
	}, descriptor.Headers)

	// Renderers run on every client read and may not fetch.
	_, err = adaptor.ConvertToOpenAIVideo(&model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess})
	assert.ErrorContains(t, err, "utils.fetch is not available in protocols.openai_video.render")
}

// The submit, query and result hooks fetch through the channel; usage hooks,
// whose facts are billed, cannot, so a fetching usage hook is a usage error.
func TestTaskAdaptorDriverHooksFetchButUsageHooksCannot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var lookups []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, found := strings.CutPrefix(r.URL.Path, "/lookup/"); found {
			lookups = append(lookups, name)
			_, _ = w.Write([]byte(`{"value":"` + name + `"}`))
			return
		}
		assert.Equal(t, "/tasks/upstream-1", r.URL.Path)
		assert.Equal(t, "query", r.Header.Get("X-Plugin"))
		_, _ = w.Write([]byte(`{"id":"upstream-1"}`))
	}))
	defer server.Close()
	source := strings.NewReplacer(
		`headers: {"X-Plugin": "submit"}`, `headers: {"X-Plugin": utils.fetch({url: ctx.baseUrl + "/lookup/submit"}).body.value}`,
		`headers: {"X-Plugin": "query"}`, `headers: {"X-Plugin": utils.fetch({url: ctx.baseUrl + "/lookup/query"}).body.value}`,
		`url: body.url}`, `url: utils.fetch({url: ctx.baseUrl + "/lookup/result"}).body.value}`,
		`return {upstreamUnits: 23};`, `return {upstreamUnits: utils.fetch({url: task.baseUrl + "/lookup/23"}).body.value};`,
	).Replace(mockPlugin)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "secret"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "hello"})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	req := httptest.NewRequest(http.MethodPost, server.URL+"/submit", nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, req, info))
	assert.Equal(t, "submit", req.Header.Get("X-Plugin"))

	task := &model.Task{Action: info.Action, PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-1"}}
	queryResp, err := adaptor.FetchTaskForTask(server.URL, "secret", task, "")
	require.NoError(t, err)
	queryBody, err := io.ReadAll(queryResp.Body)
	require.NoError(t, err)
	require.NoError(t, queryResp.Body.Close())
	result, err := adaptor.ParseTaskResultForTask(task, queryResp, queryBody)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", result.Status)
	assert.Equal(t, "result", result.Url)
	assert.Equal(t, "plugin completion usage hook failed", result.UsageError)
	assert.Nil(t, result.UsageFacts)
	assert.Equal(t, []string{"submit", "query", "result"}, lookups)

	lookups = nil
	batchServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if name, found := strings.CutPrefix(r.URL.Path, "/lookup/"); found {
			lookups = append(lookups, name)
			_, _ = w.Write([]byte(`{"value":"` + name + `"}`))
			return
		}
		assert.Equal(t, "/batch", r.URL.Path)
		assert.Equal(t, "batch", r.Header.Get("X-Plugin"))
		_, _ = w.Write([]byte(`{"items":[{"id":"task-a","status":"SUCCESS","usage":23}]}`))
	}))
	defer batchServer.Close()
	batchSource := strings.NewReplacer(
		`headers: { "X-Plugin": "batch" }`, `headers: { "X-Plugin": utils.fetch({url: ctx.baseUrl + "/lookup/batch"}).body.value }`,
		`url: (item.urls || [])[0] || ""`, `url: utils.fetch({url: ctx.baseUrl + "/lookup/" + item.id}).body.value`,
	).Replace(batchMockPlugin)
	batchPlugin, err := pluginruntime.NewRegistry().Register(batchSource, pluginruntime.Options{})
	require.NoError(t, err)
	batch := New(batchPlugin)
	batch.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: batchServer.URL, ApiKey: "secret"}})
	tasks := []*model.Task{{PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-a"}}}
	batchResp, err := batch.FetchBatchTasks(batchServer.URL, "secret", tasks, "")
	require.NoError(t, err)
	batchBody, err := io.ReadAll(batchResp.Body)
	require.NoError(t, err)
	require.NoError(t, batchResp.Body.Close())
	results, err := batch.ParseBatchResult(tasks, batchResp, batchBody)
	require.NoError(t, err)
	require.Contains(t, results, "task-a")
	assert.Equal(t, "task-a", results["task-a"].TaskInfo.Url)
	assert.Equal(t, "23", results["task-a"].TaskInfo.UsageFacts["upstreamUnits"])
	assert.Equal(t, []string{"batch", "task-a"}, lookups)
}

func TestTaskAdaptorMapsJSContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/submit":
			assert.Equal(t, "submit", r.Header.Get("X-Plugin"))
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			assert.JSONEq(t, `{"prompt":"hello"}`, string(body))
			_, _ = w.Write([]byte(`{"id":"upstream-1"}`))
		case "/tasks/upstream-1":
			assert.Equal(t, "query", r.Header.Get("X-Plugin"))
			_, _ = w.Write([]byte(`{"id":"upstream-1","url":"https://cdn.example/video.mp4"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	registry := pluginruntime.NewRegistry()
	plugin, err := registry.Register(mockPlugin, pluginruntime.Options{Key: "mock-task", Version: "1.0.0"})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "secret"}, OriginModelName: "client-model", TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
	adaptor.Init(info)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "hello"})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, "text_to_video", info.Action)
	assert.Equal(t, "mock-v1", info.OriginModelName)
	assert.Equal(t, "mock-upstream", info.UpstreamModelName)
	assert.Equal(t, []string{"mock-v1"}, adaptor.GetModelList())
	assert.Equal(t, "Mock Task", adaptor.GetChannelName())
	facts, factsErr := adaptor.ExtractUsageFactsValidated(c, info)
	require.NoError(t, factsErr)
	assert.Equal(t, "5", facts["seconds"])

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/submit", url)
	req := httptest.NewRequest(http.MethodPost, url, nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, req, info))
	assert.Equal(t, "submit", req.Header.Get("X-Plugin"))

	resp, err := adaptor.DoRequest(c, info, requestBody)
	require.NoError(t, err)
	parsed, taskErr := adaptor.ParseResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.NotNil(t, parsed)
	assert.Equal(t, "upstream-1", parsed.UpstreamTaskID)
	assert.JSONEq(t, `{"accepted":true,"status":200}`, string(parsed.TaskData))
	assert.Nil(t, parsed.ClientResponse)
	assert.Empty(t, recorder.Body.String(), "response parsing must not write before the durable task barrier")

	queryResp, err := adaptor.FetchTaskForTask(server.URL, "secret", &model.Task{
		Action:      info.Action,
		PrivateData: model.TaskPrivateData{UpstreamTaskID: parsed.UpstreamTaskID},
	}, "")
	require.NoError(t, err)
	queryBody, err := io.ReadAll(queryResp.Body)
	require.NoError(t, err)
	require.NoError(t, queryResp.Body.Close())
	result, err := adaptor.ParseTaskResultForTask(&model.Task{}, queryResp, queryBody)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", result.Status)
	assert.Equal(t, "https://cdn.example/video.mp4", result.Url)
	assert.Equal(t, "23", result.UsageFacts["upstreamUnits"])
	assert.Zero(t, result.TotalTokens)

	rendered, err := adaptor.ConvertToOpenAIVideo(&model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"id":"task_public",
		"object":"video",
		"model":"",
		"status":"completed",
		"progress":0,
		"created_at":0
	}`, string(rendered))
	_, err = plugin.Engine.Export(context.Background(), "meta")
	require.NoError(t, err)
}

func TestTaskAdaptorPreservesSoraVideoResponseFields(t *testing.T) {
	source, err := plugins.Source("sora")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	for _, tc := range []struct {
		status model.TaskStatus
		want   string
	}{
		{model.TaskStatusInProgress, "in_progress"},
		{model.TaskStatusSuccess, "completed"},
		{model.TaskStatusFailure, "failed"},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			task := &model.Task{
				TaskID:      "task_public",
				Status:      tc.status,
				Progress:    "42%",
				CreatedAt:   100,
				FinishTime:  200,
				Properties:  model.Properties{OriginModelName: "origin-model"},
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-task-id"},
				Data: []byte(`{
					"id":"upstream-task-id","task_id":"upstream-task-id",
					"object":"provider-video","model":"provider-model",
					"status":"completed","progress":100,"created_at":10,"completed_at":20,
					"url":"https://cdn.example/video.mp4",
					"metadata":{"url":"https://cdn.example/video.mp4","URL":"https://cdn.example/uppercase.mp4"},
					"provider_payload":{"task_id":"upstream-task-id","items":[{"enabled":false,"count":0,"value":null}]},
					"seconds":8,"resolution":"720p","aspect_ratio":"16:9",
					"reference_images":["https://cdn.example/reference.png"],
					"error":{"code":"provider_error","message":"provider rejected request","detail":{"retryable":false}}
				}`),
			}
			rendered, err := adaptor.ConvertToOpenAIVideo(task)
			require.NoError(t, err)

			var fields map[string]any
			require.NoError(t, common.Unmarshal(rendered, &fields))
			assert.Equal(t, "task_public", fields["id"])
			assert.NotContains(t, fields, "task_id")
			assert.Equal(t, "video", fields["object"])
			assert.Equal(t, "origin-model", fields["model"])
			assert.Equal(t, tc.want, fields["status"])
			assert.Equal(t, float64(42), fields["progress"])
			assert.Equal(t, float64(100), fields["created_at"])
			if tc.status == model.TaskStatusSuccess {
				assert.Equal(t, float64(200), fields["completed_at"])
			} else {
				assert.NotContains(t, fields, "completed_at")
			}
			assert.Equal(t, "https://cdn.example/video.mp4", fields["url"])
			assert.Equal(t, map[string]any{
				"url": "https://cdn.example/video.mp4",
				"URL": "https://cdn.example/uppercase.mp4",
			}, fields["metadata"])
			assert.Equal(t, map[string]any{
				"task_id": "task_public",
				"items":   []any{map[string]any{"enabled": false, "count": float64(0), "value": nil}},
			}, fields["provider_payload"])
			assert.Equal(t, float64(8), fields["seconds"])
			assert.Equal(t, "720p", fields["resolution"])
			assert.Equal(t, "16:9", fields["aspect_ratio"])
			assert.Equal(t, []any{"https://cdn.example/reference.png"}, fields["reference_images"])
			assert.Equal(t, map[string]any{
				"code": "provider_error", "message": "provider rejected request",
				"detail": map[string]any{"retryable": false},
			}, fields["error"])
		})
	}
}

func TestTaskAdaptorRejectsNonObjectOpenAIVideoRendererOutput(t *testing.T) {
	for value, message := range map[string]string{
		"null":           "plugin returned an invalid OpenAI video object",
		"undefined":      "plugin returned an invalid OpenAI video object",
		"[]":             "plugin returned an invalid OpenAI video object",
		`"video"`:        "plugin returned an invalid OpenAI video object",
		"42":             "plugin returned an invalid OpenAI video object",
		"false":          "plugin returned an invalid OpenAI video object",
		"{seconds: NaN}": "json: unsupported value: NaN",
		"[Infinity]":     "json: unsupported value: +Inf",
	} {
		t.Run(value, func(t *testing.T) {
			source := strings.Replace(mockPlugin, `return {id: task.task_id, status: "completed"};`, "return "+value+";", 1)
			plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
			require.NoError(t, err)
			_, err = New(plugin).ConvertToOpenAIVideo(&model.Task{TaskID: "task_public"})
			require.EqualError(t, err, message)
		})
	}
}

func TestTaskAdaptorPreservesOpenAIVideoFailureSlotsAndOwnsLifecycle(t *testing.T) {
	source := strings.Replace(mockPlugin, `render: function(ctx, task) { return {id: task.task_id, status: "completed"}; }`, `render: function() { return {id:"provider", object:"provider", model:"provider-model", status:"completed", progress:100, created_at:99, completed_at:20, error:{code:"provider_error",message:"provider rejected request"}}; }`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	task := &model.Task{
		TaskID:     "task_public",
		Status:     model.TaskStatusFailure,
		FailReason: "provider secret",
		CreatedAt:  10,
		UpdatedAt:  20,
		Properties: model.Properties{OriginModelName: "origin-model"},
	}

	rendered, err := adaptor.ConvertToOpenAIVideo(task)

	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"task_public","object":"video","model":"origin-model","status":"failed","progress":0,"created_at":10,"error":{"message":"provider rejected request","code":"provider_error"}}`, string(rendered))
}

func TestTaskAdaptorBoundsNativeUsageBeforeQuotaCalculation(t *testing.T) {
	source := `
export const meta = {
  apiVersion: 1, key: "bounded-usage", name: "Bounded Usage", version: "1.0.0",
  author: {name: "Test"},
  models: ["model"], fetchMode: "per_task",
  usageSchema: {
    duration: {type: "number", unit: "second"},
    count: {type: "number", unit: "count"},
    tokens: {type: "number", unit: "token"},
    mode: {enum: ["std", "pro"]},
  },
  usageExamples: [{label: "std · 1s", facts: {duration: 1, count: 1, tokens: 1, mode: "std"}}],
};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl + "/submit", method: "POST", body: {}};
}
export function parseSubmitResponse() { return {taskId: "1"}; }
export function buildQueryRequest() { return {url: "https://example.com"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function extractUsage(ctx) {
  const entries = (ctx.requestBody || {}).hookUsageEntries || [];
  const facts = {};
  entries.forEach(function(entry) { facts[entry.name] = entry.value; });
  return facts;
}
export function extractUsageOnSubmit(ctx, data) { return (data || {}).usage || {}; }
export function extractUsageOnComplete(task, result, body) { return (body || {}).completionUsage || {}; }
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)

	newRequest := func(t *testing.T, requestBody map[string]any) (*TaskAdaptor, *gin.Context, *relaycommon.RelayInfo) {
		t.Helper()
		adaptor := New(plugin)
		info := &relaycommon.RelayInfo{
			ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
			TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		}
		adaptor.Init(info)
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/native/submit", nil)
		context.Set("task_request", requestBody)
		return adaptor, context, info
	}

	requestTests := []struct {
		name string
		body map[string]any
	}{
		{
			name: "duration in resolved metadata",
			body: map[string]any{"metadata": map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1}},
		},
		{
			name: "count in resolved metadata",
			body: map[string]any{"metadata": map[string]any{"count": dto.MaxImageN + 1}},
		},
		{
			name: "declared enum in resolved metadata",
			body: map[string]any{"metadata": map[string]any{"mode": "turbo"}},
		},
		{
			name: "implicit duration key without declaration",
			body: map[string]any{"durationSeconds": relaycommon.MaxTaskDurationSeconds + 1},
		},
		{
			name: "implicit count key without declaration",
			body: map[string]any{"image_count": dto.MaxImageN + 1},
		},
		{
			name: "negative resolved duration",
			body: map[string]any{"duration": -1},
		},
		{
			name: "non-finite resolved duration",
			body: map[string]any{"duration": math.Inf(1)},
		},
		{
			name: "metadata cannot hide behind valid top-level duration",
			body: map[string]any{
				"duration": relaycommon.MaxTaskDurationSeconds,
				"metadata": map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1},
			},
		},
		{
			name: "nested passthrough duration",
			body: map[string]any{
				"metadata": map[string]any{
					"parameters": map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1},
				},
			},
		},
	}
	for _, testCase := range requestTests {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor, context, info := newRequest(t, testCase.body)
			taskErr := adaptor.ValidateRequestAndSetAction(context, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "plugin_usage_invalid", taskErr.Code)
		})
	}

	hookTests := []struct {
		name  string
		usage map[string]any
	}{
		{
			name:  "duration returned only by extractUsage",
			usage: map[string]any{"duration": float64(relaycommon.MaxTaskDurationSeconds + 1)},
		},
		{
			name:  "count returned only by extractUsage",
			usage: map[string]any{"count": float64(dto.MaxImageN + 1)},
		},
		{
			name:  "enum returned only by extractUsage",
			usage: map[string]any{"mode": "turbo"},
		},
		{
			name:  "undeclared numeric ratio uses conservative host ceiling",
			usage: map[string]any{"custom_ratio": float64(relaycommon.MaxTaskDurationSeconds + 1)},
		},
		{
			name:  "negative hook ratio",
			usage: map[string]any{"custom_ratio": -1.0},
		},
		{
			name:  "undeclared hook field",
			usage: map[string]any{"custom_ratio": "1"},
		},
	}
	for _, testCase := range hookTests {
		t.Run(testCase.name, func(t *testing.T) {
			entries := make([]any, 0, len(testCase.usage))
			for key, value := range testCase.usage {
				entries = append(entries, map[string]any{"name": key, "value": value})
			}
			adaptor, context, info := newRequest(t, map[string]any{"hookUsageEntries": entries})
			require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
			ratios, err := adaptor.ExtractUsageFactsValidated(context, info)
			require.Error(t, err)
			assert.Nil(t, ratios)
		})
	}

	t.Run("numeric strings remain valid in vendor request fields", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"metadata": map[string]any{
				"duration": "5",
				"count":    "2",
				"mode":     "std",
			},
		})
		assert.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
	})

	t.Run("numeric strings from usage hooks retain exact values", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{map[string]any{"name": "duration", "value": "5"}},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		ratios, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, "5", ratios["duration"])
	})

	t.Run("declared token facts use the token bound instead of duration cap", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{
				map[string]any{"name": "tokens", "value": float64(500000)},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		facts, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, "500000", facts["tokens"])

		ratios, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, "500000", ratios["tokens"])
	})

	t.Run("declared token facts reject the int32 quota overflow", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{
				map[string]any{"name": "tokens", "value": float64(common.MaxQuota) + 1},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		facts, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.Error(t, err)
		assert.Nil(t, facts)
	})

	t.Run("canonical maxima and enum are accepted", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"duration": relaycommon.MaxTaskDurationSeconds,
			"count":    dto.MaxImageN,
			"mode":     "std",
			"hookUsageEntries": []any{
				map[string]any{"name": "duration", "value": float64(relaycommon.MaxTaskDurationSeconds)},
				map[string]any{"name": "count", "value": float64(dto.MaxImageN)},
				map[string]any{"name": "mode", "value": "pro"},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		ratios, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"duration": "3600", "count": "128", "mode": "pro"}, ratios)
	})

	t.Run("runtime error does not expose plugin-controlled usage key", func(t *testing.T) {
		adaptor, context, info := newRequest(t, map[string]any{
			"hookUsageEntries": []any{
				map[string]any{
					"name":  "https://private.invalid/?token=secret",
					"value": float64(relaycommon.MaxTaskDurationSeconds + 1),
				},
			},
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))
		_, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "private.invalid")
		assert.NotContains(t, err.Error(), "secret")
	})

	for _, testCase := range []struct {
		name  string
		usage map[string]any
	}{
		{
			name:  "oversized completion duration is discarded",
			usage: map[string]any{"duration": relaycommon.MaxTaskDurationSeconds + 1},
		},
		{
			name:  "oversized completion count is discarded",
			usage: map[string]any{"count": dto.MaxImageN + 1},
		},
		{
			name:  "malformed completion decimal is discarded",
			usage: map[string]any{"duration": "NaN"},
		},
		{
			name:  "overflowing upstream units are not settlement evidence",
			usage: map[string]any{"upstreamUnits": float64(common.MaxQuota) + 1},
		},
		{
			name:  "negative token facts are not settlement evidence",
			usage: map[string]any{"completionTokens": -1, "totalTokens": 2},
		},
		{
			name:  "fractional token facts are not settlement evidence",
			usage: map[string]any{"completionTokens": 1.5, "totalTokens": 2},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor, _, _ := newRequest(t, map[string]any{})
			body, marshalErr := common.Marshal(map[string]any{"completionUsage": testCase.usage})
			require.NoError(t, marshalErr)
			result, parseErr := adaptor.ParseTaskResultForTask(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
			require.NoError(t, parseErr)
			assert.Nil(t, result.UsageFacts)
			assert.Zero(t, result.TotalTokens)
			assert.NotEmpty(t, result.UsageError)
		})
	}

	t.Run("declared completion token unit uses token bounds", func(t *testing.T) {
		adaptor, _, _ := newRequest(t, map[string]any{})
		body, err := common.Marshal(map[string]any{"completionUsage": map[string]any{"tokens": 500000}})
		require.NoError(t, err)
		result, err := adaptor.ParseTaskResultForTask(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
		require.NoError(t, err)
		assert.Equal(t, "500000", result.UsageFacts["tokens"])
	})

	t.Run("declared credit facts keep sub-integer precision", func(t *testing.T) {
		source := `
export const meta = {
  apiVersion: 1, key: "credit-decimals", name: "Credit Decimals", version: "1.0.0",
  author: {name: "Test"}, models: ["model"], fetchMode: "per_task",
  usageSchema: {units: {type: "number", unit: "credit"}},
  usageExamples: [{label: "3.5 credits", facts: {units: "3.5"}}],
};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest() { return {url: "https://example.com"}; }
export function parseTaskResult() { return {status: "SUCCESS"}; }
export function extractUsage() { return {units: "3.5"}; }
export function extractUsageOnComplete() { return {units: "3.5"}; }
`
		plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
		require.NoError(t, err)
		adaptor := New(plugin)
		info := &relaycommon.RelayInfo{
			ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
			TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		}
		adaptor.Init(info)
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodPost, "/native/submit", nil)
		context.Set("task_request", map[string]any{"model": "model"})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(context, info))

		facts, err := adaptor.ExtractUsageFactsValidated(context, info)
		require.NoError(t, err)
		assert.Equal(t, "3.5", facts["units"])

		body, err := common.Marshal(map[string]any{})
		require.NoError(t, err)
		result, err := adaptor.ParseTaskResultForTask(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
		require.NoError(t, err)
		assert.Equal(t, "3.5", result.UsageFacts["units"])
	})

	t.Run("undeclared legacy token aliases cannot be charged", func(t *testing.T) {
		adaptor, _, _ := newRequest(t, map[string]any{})
		body, err := common.Marshal(map[string]any{"completionUsage": map[string]any{"upstreamUnits": 5000}})
		require.NoError(t, err)
		result, err := adaptor.ParseTaskResultForTask(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, body)
		require.NoError(t, err)
		assert.Zero(t, result.TotalTokens)
		assert.Nil(t, result.UsageFacts)
		assert.NotEmpty(t, result.UsageError)
	})

}

func TestTaskAdaptorAcceptsNormalizedLegacyTokenCounters(t *testing.T) {
	source := `
export const meta = {apiVersion:1,key:"normalized-tokens",name:"Normalized Tokens",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url: ctx.baseUrl + "/submit"}; }
export function parseSubmitResponse() { return {taskId: "task"}; }
export function buildQueryRequest(ctx) { return {url: ctx.baseUrl + "/query"}; }
export function parseTaskResult(ctx, body) { return {status: "SUCCESS", completionTokens: body.completion, totalTokens: body.total}; }
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	result, err := adaptor.ParseTaskResultForTask(&model.Task{}, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}, []byte(`{"completion":13,"total":17}`))
	require.NoError(t, err)
	assert.Equal(t, 13, result.CompletionTokens)
	assert.Equal(t, 17, result.TotalTokens)
	assert.Nil(t, result.UsageFacts)
}

func TestSubmitContextExposesOriginTasks(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginTasks: []relaycommon.OriginTaskRef{{
				TaskID:         "task_pub_1",
				UpstreamTaskID: "cgt-upstream-1",
				Action:         "text_to_video",
				Status:         "SUCCESS",
				Data:           []byte(`{"id":"cgt-upstream-1"}`),
			}},
		},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	ctx := adaptor.submitContext(c, info)

	originTasks, ok := ctx["originTasks"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, originTasks, 1)
	assert.Equal(t, "task_pub_1", originTasks[0]["taskId"])
	assert.Equal(t, "cgt-upstream-1", originTasks[0]["upstreamTaskId"])
	assert.Equal(t, "text_to_video", originTasks[0]["action"])
	assert.Equal(t, "SUCCESS", originTasks[0]["status"])
	assert.Equal(t, map[string]any{"id": "cgt-upstream-1"}, originTasks[0]["data"])
}

func TestSubmitContextOmitsOriginTasksWhenEmpty(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	ctx := adaptor.submitContext(c, info)

	_, ok := ctx["originTasks"]
	assert.False(t, ok)
}

func TestSubmitContextOriginTasksNilDataOnInvalidJSON(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example", ApiKey: "secret"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{
			OriginTasks: []relaycommon.OriginTaskRef{{
				TaskID:         "task_pub_1",
				UpstreamTaskID: "cgt-upstream-1",
				Action:         "text_to_video",
				Status:         "SUCCESS",
				Data:           []byte("not-json"),
			}},
		},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	ctx := adaptor.submitContext(c, info)

	originTasks, ok := ctx["originTasks"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, originTasks, 1)
	assert.Nil(t, originTasks[0]["data"])
}

func TestSubmitContextNormalizesRequestBodyOnEveryCall(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(mockPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", map[string]any{"prompt": string([]byte{0xff}), "count": int64(math.MaxInt64)})

	normalized := map[string]any{"prompt": "�", "count": float64(math.MaxInt64)}
	assert.Equal(t, normalized, adaptor.submitContext(c, info)["requestBody"])
	assert.Equal(t, normalized, adaptor.submitContext(c, info)["requestBody"])
	assert.Equal(t, normalized, adaptor.submitContext(nil, info)["requestBody"])

	replaced := map[string]any{"prompt": "second", "count": int64(2)}
	c.Set("task_request", replaced)
	assert.Equal(t, replaced, adaptor.submitContext(c, info)["requestBody"])
}

func TestTaskAdaptorRejectsRequestHostOverride(t *testing.T) {
	source := strings.Replace(mockPlugin, `ctx.baseUrl + "/submit"`, `"https://attacker.example/steal"`, 1)
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{Key: "mock-task", Version: "1.0.0"})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "hello"})
	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "not allowed")
}

const batchMockPlugin = `
export const meta = { apiVersion: 1, key: "mock-batch", name: "Mock Batch", version: "1.0.0", author: {name: "Test"}, channelTypes: [1002], models: ["batch-v1"], fetchMode: "batch", usageSchema:{upstreamUnits:{type:"number",unit:"credit"}} };
export function buildSubmitRequest(ctx) { return { url: ctx.baseUrl + "/submit", method: "POST", body: {} }; }
export function parseSubmitResponse(ctx, resp) { return { taskId: resp.body.id }; }
export function buildQueryRequest(ctx) { return { url: ctx.baseUrl + "/tasks/" + ctx.taskId }; }
export function parseTaskResult(ctx, body) { return { taskId: body.id, status: "SUCCESS" }; }
export function buildBatchQueryRequest(ctx, tasks) { return { url: ctx.baseUrl + "/batch", method: "POST", headers: { "X-Plugin": "batch" }, body: { ids: (tasks || []).map(function (task) { return task.taskId; }) } }; }
export function parseBatchResult(ctx, body) {
  return body.items.map(function (item, index) {
    return { usagePath:["items",index], taskId: item.id, action: item.action, status: item.status, progress: item.progress, url: (item.urls || [])[0] || "", finishTime: item.finish || 0, data: item };
  });
}
export function extractUsageOnComplete(task, result, body) { return {upstreamUnits: body.usage || 0}; }
`

// Covers the bridge half of the batch contract: FetchBatchTasks must build the
// upstream request from the plugin descriptor, and ParseBatchResult must key
// results by taskId, preserve the explicit result URL, and skip entries without
// a task id.
func TestTaskAdaptorBatchBridge(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/batch", r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "batch", r.Header.Get("X-Plugin"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"ids":["task-a","task-b"]}`, string(body))
		_, _ = w.Write([]byte(`{"items":[
			{"id":"task-a","action":"music","status":"SUCCESS","progress":"100%","urls":["https://cdn.example/a1.mp3","https://cdn.example/a2.mp3"],"finish":1700000000,"usage":23},
			{"id":"task-b","status":"IN_PROGRESS","progress":"40%"},
			{"id":"","status":"SUCCESS"}
		]}`))
	}))
	defer server.Close()

	plugin, err := pluginruntime.NewRegistry().Register(batchMockPlugin, pluginruntime.Options{Key: "mock-batch", Version: "1.0.0"})
	require.NoError(t, err)
	adaptor := New(plugin)
	require.Equal(t, "batch", adaptor.FetchMode())

	tasks := []*model.Task{
		{PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-a"}},
		{PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-b"}},
	}
	resp, err := adaptor.FetchBatchTasks(server.URL, "secret", tasks, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	results, err := adaptor.ParseBatchResult(tasks, resp, payload)
	require.NoError(t, err)
	require.Len(t, results, 2, "entry without taskId must be skipped")

	done := results["task-a"]
	require.NotNil(t, done)
	assert.Equal(t, "music", done.Action)
	assert.Equal(t, "SUCCESS", done.TaskInfo.Status)
	assert.Equal(t, "100%", done.TaskInfo.Progress)
	assert.Equal(t, "https://cdn.example/a1.mp3", done.TaskInfo.Url)
	assert.Equal(t, int64(1700000000), done.FinishTime)
	assert.Equal(t, "23", done.TaskInfo.UsageFacts["upstreamUnits"])
	assert.Zero(t, done.TaskInfo.TotalTokens)
	require.NotNil(t, done.Data)

	pending := results["task-b"]
	require.NotNil(t, pending)
	assert.Equal(t, "IN_PROGRESS", pending.TaskInfo.Status)
	assert.Equal(t, "40%", pending.TaskInfo.Progress)
	assert.Empty(t, pending.TaskInfo.Url)
}

const mappingOrderAdaptorPlugin = `
export const meta = {apiVersion:1,key:"map-order-adaptor",name:"Map Order Adaptor",version:"1.0.0",author:{name:"Test"},models:["declared-model"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl+"/submit", method:"POST", body:{upstreamModel: ctx.upstreamModel, model: ctx.model}};
}
export function parseSubmitResponse(){return {taskId:"1"};}
export function buildQueryRequest(){return {url:"https://provider.example"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`

func mappingOrderSubmitBody(t *testing.T, origin, mapping string) []byte {
	t.Helper()
	plugin, err := pluginruntime.NewRegistry().Register(mappingOrderAdaptorPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: origin,
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	if mapping != "" {
		c.Set("model_mapping", mapping)
	}
	c.Set("task_request", map[string]any{"prompt": "p"})
	info.UpstreamModelName = info.OriginModelName
	require.NoError(t, helper.ModelMappedHelper(c, info, nil))
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	return raw
}

func TestTaskAdaptorBuildSubmitReceivesMappedUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapped := mappingOrderSubmitBody(t, "alias-model", `{"alias-model":"mid-model","mid-model":"declared-model"}`)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(mapped, &decoded))
	assert.Equal(t, "declared-model", decoded["upstreamModel"])
	assert.Equal(t, "alias-model", decoded["model"])

	withoutMapping := mappingOrderSubmitBody(t, "declared-model", "")
	emptyMapping := mappingOrderSubmitBody(t, "declared-model", "{}")
	assert.Equal(t, withoutMapping, emptyMapping)
	require.NoError(t, common.Unmarshal(withoutMapping, &decoded))
	assert.Equal(t, "declared-model", decoded["upstreamModel"])
	assert.Equal(t, "declared-model", decoded["model"])
}

// Polling has no relay info, so query hooks can only branch on the model when
// the host forwards the persisted task identities from the fetch body.
func TestTaskAdaptorFetchTaskExposesModelIdentities(t *testing.T) {
	service.InitHttpClient()
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"query-model",name:"Query Model",version:"1.0.0",author:{name:"Test"},models:["alias"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/tasks/"+ctx.model+"/"+ctx.upstreamModel+"/"+ctx.taskId,method:"GET"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)

	testCases := []struct {
		name string
		task *model.Task
		want string
	}{
		{
			name: "mapped model",
			task: &model.Task{
				Properties:  model.Properties{OriginModelName: "alias", UpstreamModelName: "declared-model"},
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "t1"},
			},
			want: "/tasks/alias/declared-model/t1",
		},
		{
			name: "unmapped model falls back to the origin name",
			task: &model.Task{
				Properties:  model.Properties{OriginModelName: "alias"},
				PrivateData: model.TaskPrivateData{UpstreamTaskID: "t1"},
			},
			want: "/tasks/alias/alias/t1",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			resp, fetchErr := adaptor.FetchTaskForTask(server.URL, "secret", testCase.task, "")
			require.NoError(t, fetchErr)
			require.NoError(t, resp.Body.Close())
			assert.Equal(t, testCase.want, requested)
		})
	}
}

func TestTaskAdaptorQueryContextOmitsRequestBody(t *testing.T) {
	service.InitHttpClient()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, common.DecodeJson(r.Body, &captured))
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"query-ctx",name:"Query Ctx",version:"1.0.0",author:{name:"Test"},models:["alias"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1",state:{req_key:"from-submit"}}}
export function buildQueryRequest(ctx){
  return {url:ctx.baseUrl+"/query",method:"POST",body:{
    keys: Object.keys(ctx).sort(),
    taskId: ctx.taskId,
    publicTaskId: ctx.publicTaskId,
    action: ctx.action,
    model: ctx.model,
    upstreamModel: ctx.upstreamModel,
    data: ctx.data,
    state: ctx.state,
    hasRequestBody: Object.prototype.hasOwnProperty.call(ctx, "requestBody")
  }};
}
export function parseTaskResult(ctx, body, response){
  return {status:"IN_PROGRESS",reason:String(response && response.status),url:ctx.taskId,state:{round:"poll"}};
}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "secret"}})

	task := &model.Task{
		TaskID: "task_public",
		Action: constant.TaskActionImageToVideo,
		Properties: model.Properties{
			OriginModelName:   "alias",
			UpstreamModelName: "declared",
		},
		Data: []byte(`{"snapshot":true}`),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-1",
			PluginState:    []byte(`{"req_key":"kept"}`),
		},
	}
	resp, err := adaptor.FetchTaskForTask(server.URL, "secret", task, "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, "upstream-1", captured["taskId"])
	assert.Equal(t, "task_public", captured["publicTaskId"])
	assert.Equal(t, constant.NormalizeTaskAction(constant.TaskActionImageToVideo), captured["action"])
	assert.Equal(t, "alias", captured["model"])
	assert.Equal(t, "declared", captured["upstreamModel"])
	assert.Equal(t, map[string]any{"snapshot": true}, captured["data"])
	assert.Equal(t, map[string]any{"req_key": "kept"}, captured["state"])
	assert.Equal(t, false, captured["hasRequestBody"])
	keys, ok := captured["keys"].([]any)
	require.True(t, ok)
	assert.NotContains(t, keys, "requestBody")

	result, err := adaptor.ParseTaskResultForTask(task, &http.Response{StatusCode: http.StatusTeapot, Header: make(http.Header)}, []byte(`{"ok":true}`))
	require.NoError(t, err)
	assert.Equal(t, "IN_PROGRESS", result.Status)
	assert.Equal(t, "418", result.Reason)
	assert.Equal(t, "upstream-1", result.Url)
	assert.JSONEq(t, `{"round":"poll"}`, string(result.PluginState))
}

func TestTaskAdaptorParseSubmitResponsePersistsState(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"upstream-1"}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"submit-state",name:"Submit State",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit",method:"POST",body:{}}}
export function parseSubmitResponse(){return {taskId:"upstream-1",state:{req_key:"from-submit"}}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL, ApiKey: "secret"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "hello"})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	resp, err := adaptor.DoRequest(c, info, body)
	require.NoError(t, err)
	parsed, taskErr := adaptor.ParseResponse(c, resp, info)
	require.Nil(t, taskErr)
	require.NotNil(t, parsed)
	assert.JSONEq(t, `{"req_key":"from-submit"}`, string(parsed.PluginState))
}

func TestTaskAdaptorBatchQueryReceivesTaskObjects(t *testing.T) {
	service.InitHttpClient()
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, common.DecodeJson(r.Body, &captured))
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	source := `
export const meta = {apiVersion:1,key:"batch-ctx",name:"Batch Ctx",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"batch"};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"}}
export function parseSubmitResponse(){return {taskId:"1"}}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/q"}}
export function parseTaskResult(){return {status:"SUCCESS"}}
export function buildBatchQueryRequest(ctx, tasks){
  return {url:ctx.baseUrl+"/batch",method:"POST",body:{
    ids: (tasks||[]).map(function(task){return task.taskId;}),
    models: (tasks||[]).map(function(task){return task.model;}),
    hasRequestBody: (tasks||[]).some(function(task){return Object.prototype.hasOwnProperty.call(task,"requestBody");})
  }};
}
export function parseBatchResult(){return [];}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	tasks := []*model.Task{
		{Properties: model.Properties{OriginModelName: "model-a"}, PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-a"}},
		{Properties: model.Properties{OriginModelName: "model-b"}, PrivateData: model.TaskPrivateData{UpstreamTaskID: "task-b"}},
	}
	resp, err := adaptor.FetchBatchTasks(server.URL, "secret", tasks, "")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, []any{"task-a", "task-b"}, captured["ids"])
	assert.Equal(t, []any{"model-a", "model-b"}, captured["models"])
	assert.Equal(t, false, captured["hasRequestBody"])
}

func TestTaskAdaptorUsageProfilesFollowExecutionModel(t *testing.T) {
	const source = `
export const meta = {
  apiVersion:1, key:"profile-usage", name:"Profile Usage", version:"1.0.0", author:{name:"Test"},
  models:["image", "video"], fetchMode:"batch",
  usageSchema:{units:{type:"number",unit:"second"}},
  usageProfiles:[
    {models:["image"],schema:{units:{type:"number",unit:"count"},mode:{enum:["image"]}}},
    {models:["video"],schema:{units:{type:"number",unit:"token"},mode:{enum:["video"]}},
     examples:[{label:"video",facts:{units:1,mode:"video"}}]}
  ]
};
export function buildSubmitRequest(ctx) {
  return {url:ctx.baseUrl+"/submit",rewriteModel:ctx.requestBody.rewriteTo || ""};
}
export function parseSubmitResponse(){return {taskId:"task"};}
export function buildQueryRequest(){return {url:"https://provider.example/task"};}
export function buildBatchQueryRequest(){return {url:"https://provider.example/tasks"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
export function parseBatchResult(ctx,body){return body.items.map(function(item,index){return Object.assign({},item,{usagePath:["items",index,"data"]});});}
export function extractUsage(ctx){return {units:ctx.requestBody.hookUnits,mode:ctx.requestBody.hookMode};}
export function extractUsageOnSubmit(ctx,body){return body.usage;}
export function extractUsageOnComplete(ctx,result,body){return body.usage;}
`
	plugin, err := pluginruntime.NewRegistry().Register(source, pluginruntime.Options{})
	require.NoError(t, err)
	newRequest := func(t *testing.T, origin, upstream string, body map[string]any) (*TaskAdaptor, *gin.Context, *relaycommon.RelayInfo) {
		t.Helper()
		info := &relaycommon.RelayInfo{
			OriginModelName: origin,
			ChannelMeta: &relaycommon.ChannelMeta{
				UpstreamModelName: upstream, ChannelBaseUrl: "https://provider.example",
			},
			TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		}
		adaptor := New(plugin)
		adaptor.Init(info)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/native/submit", nil)
		c.Set("task_request", body)
		return adaptor, c, info
	}

	for _, tc := range []struct {
		name, origin, upstream, rewrite, mode string
		units                                 float64
		wantError                             bool
	}{
		{"mapped image", "public-alias", "image", "", "image", 2, false},
		{"mapped video token units", "public-alias", "video", "", "video", 500000, false},
		{"rewritten model", "public-alias", "image", "video", "video", 500000, false},
		{"image count ceiling", "public-alias", "image", "", "image", float64(dto.MaxImageN + 1), true},
		{"profile enum", "public-alias", "image", "", "video", 2, true},
		{"endpoint upstream keeps the declared origin profile", "image", "ep-endpoint", "", "image", 2, false},
		{"endpoint upstream enforces the origin count ceiling", "image", "ep-endpoint", "", "image", float64(dto.MaxImageN + 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adaptor, c, info := newRequest(t, tc.origin, tc.upstream, map[string]any{
				"hookUnits": tc.units, "hookMode": tc.mode, "rewriteTo": tc.rewrite,
			})
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			facts, err := adaptor.ExtractUsageFactsValidated(c, info)
			if tc.wantError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"units": strconv.FormatFloat(tc.units, 'f', -1, 64), "mode": tc.mode}, facts)
			body, err := common.Marshal(map[string]any{"usage": facts})
			require.NoError(t, err)
			assert.NotEmpty(t, body)
		})
	}

	t.Run("request revalidated after model rewrite", func(t *testing.T) {
		adaptor, c, info := newRequest(t, "public-alias", "video", map[string]any{
			"rewriteTo": "image", "metadata": map[string]any{"units": dto.MaxImageN + 1},
		})
		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "plugin_usage_invalid", taskErr.Code)
	})

	t.Run("request enum belongs to the rewritten model", func(t *testing.T) {
		adaptor, c, info := newRequest(t, "public-alias", "video", map[string]any{
			"rewriteTo": "image", "mode": "image", "units": 2,
		})
		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
		assert.Equal(t, "image", info.UpstreamModelName)
	})

	t.Run("completion selects each persisted model including legacy origin fallback", func(t *testing.T) {
		adaptor, _, _ := newRequest(t, "public-alias", "image", map[string]any{})
		for _, tc := range []struct {
			upstream, origin, mode string
			units                  float64
			accepted               bool
		}{
			{"video", "public-alias", "video", 500000, true},
			{"image", "public-alias", "image", 2, true},
			{"", "video", "video", 500000, true},
			{"image", "public-alias", "image", float64(dto.MaxImageN + 1), false},
			{"video", "public-alias", "image", 1, false},
			{"ep-endpoint", "image", "image", 2, true},
			{"ep-endpoint", "image", "image", float64(dto.MaxImageN + 1), false},
		} {
			task := &model.Task{Properties: model.Properties{OriginModelName: tc.origin, UpstreamModelName: tc.upstream}}
			body, err := common.Marshal(map[string]any{"usage": map[string]any{"units": tc.units, "mode": tc.mode}})
			require.NoError(t, err)
			result, err := adaptor.ParseTaskResultForTask(task, &http.Response{StatusCode: http.StatusOK}, body)
			require.NoError(t, err)
			if tc.accepted {
				assert.Equal(t, map[string]any{"units": strconv.FormatFloat(tc.units, 'f', -1, 64), "mode": tc.mode}, result.UsageFacts)
			} else {
				assert.Nil(t, result.UsageFacts)
			}
		}
	})

	t.Run("undeclared completion facts cannot drive stored expressions", func(t *testing.T) {
		updated, err := pluginruntime.NewRegistry().Register(strings.Replace(source,
			`export function extractUsageOnComplete(ctx,result,body){return body.usage;}`,
			`export function extractUsageOnComplete(){return {legacyRatio:3};}`, 1), pluginruntime.Options{})
		require.NoError(t, err)
		_, _, info := newRequest(t, "public-alias", "image", map[string]any{})
		adaptor := New(updated)
		adaptor.Init(info)
		result, err := adaptor.ParseTaskResultForTask(&model.Task{Properties: model.Properties{UpstreamModelName: "image"}},
			&http.Response{StatusCode: http.StatusOK}, []byte(`{}`))
		require.NoError(t, err)
		_, _, err = billingexpr.RunExprWithRequest(`tier("legacy", u("legacyRatio") * 2)`,
			billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: result.UsageFacts})
		require.Error(t, err)
		assert.NotEmpty(t, result.UsageError)
	})

	t.Run("mixed batch selects schema by task rather than adaptor", func(t *testing.T) {
		adaptor, _, _ := newRequest(t, "public-alias", "image", map[string]any{})
		tasks := []*model.Task{
			{TaskID: "video-task", Properties: model.Properties{UpstreamModelName: "video"}},
			{TaskID: "image-task", Properties: model.Properties{UpstreamModelName: "image"}},
			{TaskID: "invalid-image", Properties: model.Properties{UpstreamModelName: "image"}},
			{TaskID: "endpoint-image", Properties: model.Properties{OriginModelName: "image", UpstreamModelName: "ep-endpoint"}},
		}
		body := []byte(`{"items":[
{"taskId":"image-task","status":"SUCCESS","data":{"usage":{"units":2,"mode":"image"}}},
{"taskId":"video-task","status":"SUCCESS","data":{"usage":{"units":500000,"mode":"video"}}},
{"taskId":"invalid-image","status":"SUCCESS","data":{"usage":{"units":129,"mode":"image"}}},
{"taskId":"endpoint-image","status":"SUCCESS","data":{"usage":{"units":129,"mode":"image"}}}
]}`)
		results, err := adaptor.ParseBatchResult(tasks, &http.Response{StatusCode: http.StatusOK}, body)
		require.NoError(t, err)
		require.Len(t, results, 4)
		assert.Nil(t, results["endpoint-image"].TaskInfo.UsageFacts, "endpoint upstream must keep the origin profile's count ceiling")
		assert.Equal(t, map[string]any{"units": "500000", "mode": "video"}, results["video-task"].TaskInfo.UsageFacts)
		assert.Equal(t, map[string]any{"units": "2", "mode": "image"}, results["image-task"].TaskInfo.UsageFacts)
		assert.Nil(t, results["invalid-image"].TaskInfo.UsageFacts)
	})
}

const documentStreamPlugin = `
export const meta = {apiVersion:1,key:"document-stream",name:"Document stream",version:"1.0.0",author:{name:"Test"},models:["document"],fetchMode:"per_task",submitResponseTypes:["json","sse"],usageSchema:{units:{type:"number",unit:"count"}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/compile",responseType:ctx.requestBody.responseType || "sse"};}
export function parseSubmitEvent(ctx,event,previous) {
  const chunk = JSON.parse(event.data);
  if (chunk.error) throw new Error("provider stream failure");
  if (chunk.badState) return {state:null};
  if (chunk.largeState) return {state:{document:"x".repeat(1048577)},done:chunk.complete === true};
  if (chunk.escapedState) return {state:{document:"<".repeat(200000)},done:true};
  if (chunk.resetState) return {state:{document:"",units:0},done:true};
  const state = Object.assign({}, previous || {document:"",units:0});
  state.document += chunk.part || "";
  if (chunk.units !== undefined) state.units = chunk.units;
  state.event = event.event; state.id = event.id;
  return {state:state,done:chunk.complete === true};
}
export function parseSubmitResponse(ctx,response){return {taskId:"vendor-document",taskData:response.body,immediate:{status:"SUCCESS"},state:{revision:1}};}
export function parseTaskResult(){return {status:"SUCCESS"};}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"};}
export function extractUsageOnComplete(ctx,result,body){
  if(ctx.model!=="alias" || ctx.upstreamModel!=="document" || ctx.taskId!=="vendor-document" || ctx.publicTaskId!=="public-document" || ctx.state.revision!==1 || ctx.data.document!==body.document) throw new Error("invalid completion context");
  return {units:body.units};
}
`

func TestTaskSubmitStreamContract(t *testing.T) {
	plugin, err := pluginruntime.CompilePlugin(documentStreamPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, body, contentType string
		valid                   bool
	}{
		{"multiline and CRLF", ": heartbeat\r\nid: doc-1\r\nevent: update\r\ndata: {\"part\":\r\ndata: \"hello\",\"units\":2}\r\n\r\ndata: {\"part\":\"world\",\"units\":3,\"complete\":true}\n\n", "text/event-stream; charset=utf-8", true},
		{"zero actual units", "data: {\"part\":\"free\",\"units\":0,\"complete\":true}\n\n", "text/event-stream", true},
		{"premature EOF", "data: {\"part\":\"partial\"}\n\n", "text/event-stream", false},
		{"unterminated event", "data: {\"complete\":true}", "text/event-stream", false},
		{"provider error", "data: {\"error\":true}\n\n", "text/event-stream", false},
		{"invalid hook result", "data: {\"badState\":true}\n\n", "text/event-stream", false},
		{"state limit", "data: {\"largeState\":true}\n\n", "text/event-stream", false},
		{"intermediate state limit", "data: {\"largeState\":true}\n\ndata: {\"resetState\":true}\n\n", "text/event-stream", false},
		{"encoded state limit", "data: {\"escapedState\":true}\n\n", "text/event-stream", false},
		{"event limit", "data: " + strings.Repeat("x", maxTaskPluginPersistedJSONBytes) + "\n\n", "text/event-stream", false},
		{"wrong response type", "{}", "application/json", false},
		{"undeclared stream", "data: {}\n\n", "text/event-stream", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "alias", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "document", ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "public-document"}}
			adaptor := New(plugin)
			adaptor.Init(info)
			c, recorder := gin.CreateTestContext(httptest.NewRecorder())
			_ = recorder
			c.Request = httptest.NewRequest(http.MethodPost, "/compile", nil)
			requestBody := map[string]any{}
			if tc.name == "undeclared stream" {
				requestBody["responseType"] = "json"
			}
			c.Set("task_request", requestBody)
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			parsed, taskErr := adaptor.ParseResponse(c, response, info)
			assert.False(t, c.Writer.Written())
			if !tc.valid {
				require.NotNil(t, taskErr)
				assert.True(t, taskErr.NoRetry)
				return
			}
			require.Nil(t, taskErr)
			require.NotNil(t, parsed.Immediate)
			if tc.name == "zero actual units" {
				assert.Equal(t, map[string]any{"units": "0"}, parsed.Immediate.UsageFacts)
				return
			}
			assert.Equal(t, map[string]any{"units": "3"}, parsed.Immediate.UsageFacts)
			assert.JSONEq(t, `{"document":"helloworld","units":3,"event":"message","id":"doc-1"}`, string(parsed.TaskData))
		})
	}
}

func TestTaskSubmitStreamCancellationClosesReader(t *testing.T) {
	plugin, err := pluginruntime.CompilePlugin(documentStreamPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = New(plugin).readSubmitEvents(ctx, &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil)
	require.ErrorIs(t, err, context.Canceled)
	_, err = writer.Write([]byte("data: {}\n\n"))
	require.Error(t, err)
}

func TestTaskSubmitStreamIdleTimeout(t *testing.T) {
	previous := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = previous })
	plugin, err := pluginruntime.CompilePlugin(documentStreamPlugin, pluginruntime.Options{})
	require.NoError(t, err)
	reader, writer := io.Pipe()
	defer writer.Close()
	_, err = New(plugin).readSubmitEvents(t.Context(), &http.Response{Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil)
	require.ErrorContains(t, err, "idle timeout")
}

func TestPluginJSONValuesPreserveCodecNormalizationAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		value any
		plain bool
	}{
		{map[string]any{"units": int64(3), "nested": []any{true, "<image> / 图像", math.Copysign(0, -1), nil}}, true},
		{map[string]any{"empty": []any{}, "object": map[string]any(nil)}, true},
		{map[string]any{"null": []any(nil)}, false},
		{map[string]any{"large": int64(math.MaxInt64), "invalid UTF-8": string([]byte{0xff, 0xfe})}, false},
		{map[string]any{string([]byte{0xff}): "invalid key"}, false},
		{map[string]any{"bytes": []byte{0, 1, 255}, "number": json.Number("9007199254740993")}, false},
		{json.RawMessage(`{"units":2,"enabled":false}`), false},
		{struct {
			Units int `json:"units"`
		}{Units: 0}, false},
	} {
		encoded, err := common.Marshal(tc.value)
		require.NoError(t, err)
		var expected any
		require.NoError(t, common.Unmarshal(encoded, &expected))
		assert.Equal(t, expected, jsonValue(tc.value))
		assert.Equal(t, tc.plain, isPlainJSONValue(tc.value, 0))
	}
	source := map[string]any{"items": []any{map[string]any{"label": "original"}}}
	copy := jsonValue(source).(map[string]any)
	copy["items"].([]any)[0].(map[string]any)["label"] = "changed"
	assert.Equal(t, "original", source["items"].([]any)[0].(map[string]any)["label"])
}

func TestRequestDescriptorDecodingMatchesCodec(t *testing.T) {
	engine, err := pluginruntime.Compile(`
class Descriptor { constructor() { this.url = "u"; this.method = "PUT"; } }
let deep = "leaf";
for (let i = 0; i < 70; i++) deep = [deep];
const results = {
  "plain body": () => ({url: "https://provider.example/submit", method: "POST", headers: {"X-Plugin": "submit"}, body: {
    units: 3, large: 2 ** 60, zero: -0, text: "<image> &   图像", missing: undefined,
    items: [null, true, 1.5, {}, [], undefined],
  }}),
  "argument body": (arg) => ({url: "u", body: arg}),
  "argument headers": (arg) => ({url: "u", headers: arg.headers}),
  "changed argument body": (arg) => { arg.units = 4; return {url: "u", body: arg}; },
  "string body": () => ({url: "u", body: '{"raw":true}'}),
  "null body": () => ({url: "u", body: null}),
  "undefined body": () => ({url: "u", body: undefined}),
  "no body": () => ({url: "u", credentialless: true}),
  "class instance": () => new Descriptor(),
  "null prototype": () => Object.assign(Object.create(null), {url: "u", body: {a: 1}}),
  "getter body": () => ({url: "u", get body() { return {from: "getter"}; }}),
  "date body": () => ({url: "u", body: {at: new Date(0)}}),
  "typed array body": () => ({url: "u", body: new Uint8Array([0, 1, 255])}),
  "deep body": () => ({url: "u", body: deep}),
  "capitalized body key": () => ({url: "u", Body: {from: "Body"}}),
  "body and capitalized body": () => ({url: "u", body: {from: "body"}, Body: {from: "Body"}, BODY: "upper"}),
  "case-insensitive other field": () => ({URL: "u", Method: "PUT", body: "text"}),
  "NaN body": () => ({url: "u", body: {ratio: NaN}}),
  "infinite method": () => ({url: "u", method: Infinity, body: {}}),
  "cyclic body": () => { const body = {}; body.self = body; return {url: "u", body}; },
  "invalid header": () => ({url: "u", headers: {x: 1}, body: {}}),
  "invalid parts": () => ({url: "u", parts: "none", body: {}}),
  "not an object": () => "descriptor",
  "throwing getter": () => ({url: "u", get body() { throw new Error("getter failed"); }}),
};
export function build(name, arg) { return results[name](arg); }
`, pluginruntime.Options{})
	require.NoError(t, err)
	argument := func() map[string]any {
		return map[string]any{
			"units": int64(3), "large": int64(math.MaxInt64), "zero": math.Copysign(0, -1),
			"headers": map[string]any{"X-Plugin": "submit"},
			"items":   []any{nil, true, 1.5, map[string]any{"label": "original"}, []any{}},
		}
	}
	for _, name := range []string{
		"plain body", "argument body", "argument headers", "changed argument body", "string body", "null body",
		"undefined body", "no body", "class instance", "null prototype", "getter body", "date body",
		"typed array body", "deep body", "capitalized body key", "body and capitalized body",
		"case-insensitive other field", "NaN body", "infinite method", "cyclic body", "invalid header",
		"invalid parts", "not an object", "throwing getter",
	} {
		var expected requestDescriptor
		value, expectedErr := engine.Call(t.Context(), "build", name, argument())
		hookFailed := expectedErr != nil
		if !hookFailed {
			expectedErr = convert(value, &expected)
		}
		var decoded requestDescriptor
		err := engine.CallInto(t.Context(), &decoded, "build", name, argument())
		if expectedErr != nil {
			require.EqualError(t, err, expectedErr.Error(), name)
			var invalid *pluginruntime.ResultError
			assert.Equal(t, !hookFailed, errors.As(err, &invalid), name)
			continue
		}
		require.NoError(t, err, name)
		assert.Equal(t, expected, decoded, name)
		expectedJSON, err := common.Marshal(expected)
		require.NoError(t, err, name)
		decodedJSON, err := common.Marshal(decoded)
		require.NoError(t, err, name)
		assert.Equal(t, string(expectedJSON), string(decodedJSON), name)
	}
	source := argument()
	var decoded requestDescriptor
	require.NoError(t, engine.CallInto(t.Context(), &decoded, "build", "argument body", source))
	decoded.Body.(map[string]any)["items"].([]any)[3].(map[string]any)["label"] = "changed"
	assert.Equal(t, "original", source["items"].([]any)[3].(map[string]any)["label"])
}

func TestTaskSubmitHooksReceiveIndependentRequestSnapshots(t *testing.T) {
	source := `
export const meta={apiVersion:1,key:"request-copy",name:"Request copy",version:"1.0.0",author:{name:"Test"},models:["copy"],fetchMode:"per_task",usageSchema:{units:{type:"number",unit:"count"}}};
export function buildSubmitRequest(ctx){ctx.requestBody.units=3;ctx.requestBody.nested.label="built";return {url:ctx.baseUrl+"/submit",body:ctx.requestBody};}
export function extractUsage(ctx){if(ctx.requestBody.nested.label!=="original")throw new Error("request changed");return {units:ctx.requestBody.units};}
export function parseSubmitResponse(){return {taskId:"copy"};}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
`
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	request := map[string]any{"units": int64(2), "nested": map[string]any{"label": "original"}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/submit", nil)
	c.Set("task_request", request)
	info := &relaycommon.RelayInfo{OriginModelName: "copy", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "copy", ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor := New(plugin)
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	facts, err := adaptor.ExtractUsageFactsValidated(c, info)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"units": "2"}, facts)
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"units":3,"nested":{"label":"built"}}`, string(encoded))
	assert.Equal(t, int64(2), request["units"])
	assert.Equal(t, "original", request["nested"].(map[string]any)["label"])
}

func TestTaskSubmitDeltaStreamContract(t *testing.T) {
	source := strings.Replace(documentStreamPlugin, `submitResponseTypes:["json","sse"],`, `submitResponseTypes:["json","sse"],requiredCapabilities:["submit-sse-delta@1"],`, 1) + `
export function parseSubmitEventDelta(ctx,event,previous) {
  if(previous && previous.document !== undefined) throw new Error("full result leaked into control state");
  const chunk=JSON.parse(event.data);
  let changes=chunk.changes;
  if(chunk.largeResult) changes=[{op:"set",path:[],value:{document:"<".repeat(200000)}}];
  if(chunk.resetAfterLarge) changes.push({op:"set",path:[],value:{document:"",units:0}});
  const result={changes:changes,state:{events:(previous ? previous.events : 0)+1},done:chunk.complete===true};
  if(chunk.largeControl) result.state={text:"x".repeat(65537)};
  if(chunk.extra) result.extra=true;
  return result;
}`
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	const first = `{"changes":[{"op":"set","path":[],"value":{"document":"hello","units":2}}]}`
	const last = `{"changes":[{"op":"appendText","path":["document"],"value":"world"},{"op":"set","path":["units"],"value":0}],"complete":true}`
	for _, tc := range []struct {
		name   string
		frames []string
		valid  bool
	}{
		{"control state and zero usage", []string{first, last}, true},
		{"oversized intermediate result", []string{`{"largeResult":true}`, last}, false},
		{"oversized operation before reset", []string{`{"largeResult":true,"resetAfterLarge":true,"complete":true}`}, false},
		{"control state limit", []string{first, `{"changes":[],"largeControl":true,"complete":true}`}, false},
		{"missing changes", []string{`{"complete":true}`}, false},
		{"extra result fields", []string{first, `{"changes":[],"extra":true,"complete":true}`}, false},
		{"unfinished delta stream", []string{first}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stream strings.Builder
			for _, frame := range tc.frames {
				stream.WriteString("data: " + frame + "\n\n")
			}
			info := &relaycommon.RelayInfo{OriginModelName: "alias", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "document", ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "public-document"}}
			adaptor := New(plugin)
			adaptor.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/compile", nil)
			c.Set("task_request", map[string]any{})
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			response := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream.String()))}
			parsed, taskErr := adaptor.ParseResponse(c, response, info)
			assert.False(t, c.Writer.Written())
			if !tc.valid {
				require.NotNil(t, taskErr)
				assert.True(t, taskErr.NoRetry)
				return
			}
			require.Nil(t, taskErr)
			require.NotNil(t, parsed.Immediate)
			assert.JSONEq(t, `{"document":"helloworld","units":0}`, string(parsed.TaskData))
			assert.Equal(t, map[string]any{"units": "0"}, parsed.Immediate.UsageFacts)
		})
	}
}

func TestAlibabaSubmitDeltaDoesNotMutateControlState(t *testing.T) {
	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	const before = `{"request_id":"old","usage":{"image_count":1},"output":{"finished":false,"choices":[{"message":{"role":"assistant","content":[{"text":"hello"}]}}]}}`
	const control = `{"choices":[{"count":1,"lastText":true,"finishReason":""}],"hasUsage":true}`
	var previous, initial any
	require.NoError(t, common.UnmarshalJsonStr(control, &previous))
	require.NoError(t, common.UnmarshalJsonStr(before, &initial))
	accumulated := pluginruntime.NewJSONState(maxTaskPluginPersistedJSONBytes)
	require.NoError(t, accumulated.Apply(t.Context(), []any{map[string]any{"op": "set", "path": []any{}, "value": initial}}))
	value, err := plugin.Engine.Call(t.Context(), "parseSubmitEventDelta", map[string]any{}, map[string]any{
		"event": "message", "data": `{"request_id":"new","usage":{"image_count":2},"output":{"choices":[{"finish_reason":"stop","message":{"content":[{"text":" world"},{"image":"https://cdn.example/image.png"}]}}]}}`,
	}, previous)
	require.NoError(t, err)
	unchanged, err := common.Marshal(previous)
	require.NoError(t, err)
	assert.JSONEq(t, control, string(unchanged))
	result := value.(map[string]any)
	assert.Equal(t, true, result["done"])
	require.NoError(t, accumulated.Apply(t.Context(), result["changes"]))
	final, err := accumulated.Value()
	require.NoError(t, err)
	encoded, err := common.Marshal(final)
	require.NoError(t, err)
	assert.JSONEq(t, `{"request_id":"new","usage":{"image_count":2},"output":{"finished":true,"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":[{"text":"hello world"},{"image":"https://cdn.example/image.png"}]}}]}}`, string(encoded))
	encoded, err = common.Marshal(result["state"])
	require.NoError(t, err)
	assert.JSONEq(t, `{"choices":[{"count":2,"lastText":false,"finishReason":"stop"}],"hasUsage":true}`, string(encoded))
}

// newAPIGateway emulates the upstream New API instance a type-60 channel
// points at: it answers only on the plugin's prefixed native routes, requires
// the gateway token as a Bearer header, and renders what its own presenters
// render (the public task id already substituted into the id fields).
func newAPIGateway(t *testing.T, routes map[string]string, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer sk-gateway" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestTaskAdaptorChainsDoubaoThroughNewAPIUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var seen []string
	gateway := newAPIGateway(t, map[string]string{
		"POST /doubao/api/v3/contents/generations/tasks":               `{"id":"task_up_public"}`,
		"GET /doubao/api/v3/contents/generations/tasks/task_up_public": `{"id":"task_up_public","status":"succeeded","content":{"video_url":"https://cdn.example/v.mp4"},"usage":{"completion_tokens":1200,"total_tokens":1200}}`,
	}, &seen)
	defer gateway.Close()

	source, err := plugins.Source("doubao")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: "doubao"})
	require.NoError(t, err)
	adaptor := New(plugin)
	const modelName = "doubao-seedance-1-0-pro-250528"
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeNewAPI, ChannelBaseUrl: gateway.URL, ApiKey: "sk-gateway", UpstreamModelName: modelName},
		OriginModelName: modelName,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_local"},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "a cat", Model: modelName})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	submitURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, gateway.URL+"/doubao/api/v3/contents/generations/tasks", submitURL)
	resp, err := adaptor.DoRequest(c, info, requestBody)
	require.NoError(t, err)
	parsed, taskErr := adaptor.ParseResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "task_up_public", parsed.UpstreamTaskID, "the gateway's presenter emits its public id in the id field doubao reads")

	task := &model.Task{
		Action:      info.Action,
		Properties:  model.Properties{OriginModelName: modelName, UpstreamModelName: modelName},
		PrivateData: model.TaskPrivateData{UpstreamTaskID: parsed.UpstreamTaskID},
	}
	queryResp, err := adaptor.FetchTaskForTask(gateway.URL, "sk-gateway", task, "")
	require.NoError(t, err)
	queryBody, err := io.ReadAll(queryResp.Body)
	require.NoError(t, err)
	require.NoError(t, queryResp.Body.Close())
	result, err := adaptor.ParseTaskResultForTask(task, queryResp, queryBody)
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", result.Status)
	assert.Equal(t, "https://cdn.example/v.mp4", result.Url)
	assert.Equal(t, "1200", result.UsageFacts["tokens"])
	assert.Zero(t, result.TotalTokens)
	assert.Equal(t, []string{
		"POST /doubao/api/v3/contents/generations/tasks",
		"GET /doubao/api/v3/contents/generations/tasks/task_up_public",
	}, seen)
}

func TestTaskAdaptorChainsSunoBatchFetchThroughNewAPIUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service.InitHttpClient()
	var seen []string
	gateway := newAPIGateway(t, map[string]string{
		"POST /suno/submit/MUSIC": `{"code":"success","message":"","data":"task_up_public"}`,
		"POST /suno/fetch":        `{"code":"success","message":"","data":[{"task_id":"task_up_public","status":"SUCCESS","progress":"100%","data":[{"id":"clip-1","status":"complete","audio_url":"https://cdn.example/a.mp3"}]}]}`,
	}, &seen)
	defer gateway.Close()

	source, err := plugins.Source("sunoapi")
	require.NoError(t, err)
	plugin, err := pluginruntime.NewRegistry().RegisterFactory(source, pluginruntime.Options{Key: "sunoapi"})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeNewAPI, ChannelBaseUrl: gateway.URL, ApiKey: "sk-gateway", UpstreamModelName: "suno_music"},
		OriginModelName: "suno_music",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_local", Action: "MUSIC"},
	}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/suno/submit/music", nil)
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "a song about cats"})
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	requestBody, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	submitURL, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, gateway.URL+"/suno/submit/MUSIC", submitURL)
	resp, err := adaptor.DoRequest(c, info, requestBody)
	require.NoError(t, err)
	parsed, taskErr := adaptor.ParseResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "task_up_public", parsed.UpstreamTaskID)

	task := &model.Task{
		Action:      "MUSIC",
		Properties:  model.Properties{OriginModelName: "suno_music", UpstreamModelName: "suno_music"},
		PrivateData: model.TaskPrivateData{UpstreamTaskID: parsed.UpstreamTaskID},
	}
	batchResp, err := adaptor.FetchBatchTasks(gateway.URL, "sk-gateway", []*model.Task{task}, "")
	require.NoError(t, err)
	batchBody, err := io.ReadAll(batchResp.Body)
	require.NoError(t, err)
	require.NoError(t, batchResp.Body.Close())
	results, err := adaptor.ParseBatchResult([]*model.Task{task}, batchResp, batchBody)
	require.NoError(t, err)
	require.Contains(t, results, "task_up_public")
	assert.Equal(t, "SUCCESS", results["task_up_public"].TaskInfo.Status)
	assert.Equal(t, []string{"POST /suno/submit/MUSIC", "POST /suno/fetch"}, seen)
}
