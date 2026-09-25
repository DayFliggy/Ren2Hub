package jsplugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskAdaptorCompletionCountersPreserveInvalidUsage(t *testing.T) {
	const source = `
export const meta = {apiVersion:1,key:"completion-counters",name:"Completion",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url:ctx.baseUrl+"/submit"}; }
function result(body) { return {taskId:"upstream-job",status:"SUCCESS",completionTokens:body.completion,totalTokens:body.total}; }
export function parseSubmitResponse(ctx, response) { return {taskId:"upstream-job",immediate:result(response.body)}; }
export function buildQueryRequest(ctx) { return {url:ctx.baseUrl+"/query"}; }
export function parseTaskResult(ctx, body) { return result(body); }
export function parseBatchResult(ctx, body) { return [result(body)]; }
`
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	for _, mode := range []string{"poll", "batch", "immediate"} {
		for _, test := range []struct {
			name              string
			completion, total float64
			invalid           bool
		}{
			{"valid", 13, 17, false},
			{"negative_completion", -1, 17, true},
			{"negative_total", 0, -1, true},
			{"overflowing_completion", float64(common.MaxQuota) + 1, float64(common.MaxQuota) + 1, true},
			{"overflowing_total", 13, float64(common.MaxQuota) + 1, true},
			{"fractional_completion", 1.5, 17, true},
		} {
			t.Run(mode+"/"+test.name, func(t *testing.T) {
				adaptor := New(plugin)
				body, err := common.Marshal(map[string]any{"completion": test.completion, "total": test.total})
				require.NoError(t, err)
				var result *relaycommon.TaskInfo
				switch mode {
				case "poll":
					result, err = adaptor.ParseTaskResultForTask(&model.Task{}, nil, body)
					require.NoError(t, err)
				case "batch":
					results, err := adaptor.ParseBatchResult([]*model.Task{{TaskID: "public-job", PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-job"}}}, nil, body)
					require.NoError(t, err)
					require.Contains(t, results, "upstream-job")
					result = &results["upstream-job"].TaskInfo
				case "immediate":
					info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
					adaptor.Init(info)
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/submit", nil)
					response, taskErr := adaptor.ParseResponse(c, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, info)
					require.Nil(t, taskErr)
					require.NotNil(t, response)
					result = response.Immediate
				}
				require.NotNil(t, result)
				assert.Equal(t, "SUCCESS", result.Status, "retain terminal evidence for reconciliation")
				if test.invalid {
					assert.NotEmpty(t, result.UsageError)
					assert.Zero(t, result.CompletionTokens)
					assert.Zero(t, result.TotalTokens)
				} else {
					assert.Empty(t, result.UsageError)
					assert.EqualValues(t, test.completion, result.CompletionTokens)
					assert.EqualValues(t, test.total, result.TotalTokens)
				}
			})
		}
	}
}

func TestTaskAdaptorImmediateCompletionUsageFailureRequiresReconciliation(t *testing.T) {
	const source = `
export const meta = {apiVersion:1,key:"completion-failure",name:"Completion",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(ctx) { return {url:ctx.baseUrl+"/submit"}; }
export function parseSubmitResponse() { return {taskId:"upstream-job",immediate:{status:"SUCCESS",completionTokens:13,totalTokens:17}}; }
export function buildQueryRequest(ctx) { return {url:ctx.baseUrl+"/query"}; }
export function parseTaskResult() { return {status:"SUCCESS"}; }
export function extractUsageOnComplete() { throw new Error("provider usage unavailable"); }
`
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
	adaptor.Init(info)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/submit", nil)
	response, taskErr := adaptor.ParseResponse(c, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, info)
	require.Nil(t, taskErr)
	require.NotNil(t, response.Immediate)
	assert.Equal(t, "SUCCESS", response.Immediate.Status)
	assert.Equal(t, 17, response.Immediate.TotalTokens)
	assert.NotEmpty(t, response.Immediate.UsageError, "a hook failure cannot settle using other available counters")
	assert.Empty(t, response.Immediate.UsageFacts)
}
