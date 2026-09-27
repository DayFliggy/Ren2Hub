package jsplugin

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHailuoPollingKeepsExactCompletedSeconds(t *testing.T) {
	source, err := plugins.Source("hailuo")
	require.NoError(t, err)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	adaptor := New(plugin)
	result, err := adaptor.ParseTaskResultForTask(&model.Task{}, nil, []byte(`{"task":{"id":"1","status":"succeeded","resolution":"2K","usage":{"output_seconds":5,"input_seconds":7.500000000000000001,"input_image_count":6}}}`))
	require.NoError(t, err)
	require.Empty(t, result.UsageError)
	assert.Equal(t, "SUCCESS", result.Status)
	assert.Equal(t, map[string]any{"seconds": "5", "input_video_seconds": "7.500000000000000001", "input_images": "6", "resolution": "2K"}, result.UsageFacts)
}

const exactCompletionPlugin = `
export const meta={apiVersion:1,key:"exact-completion",name:"Exact completion",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task",usageSchema:{units:{type:"number",unit:"credit"}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"};}
export function parseSubmitResponse(ctx,resp){return {taskId:"job",taskData:resp.body,immediate:{status:"SUCCESS"}};}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"};}
export function parseTaskResult(ctx,body){return {status:"SUCCESS"};}
export function parseBatchResult(ctx,body){return body.items.map(function(item,index){return {taskId:item.id,status:"SUCCESS",data:item.data,usagePath:["items",index,"data"]};});}
export function extractUsageOnComplete(ctx,result,body){return {units:body.units};}
`

func TestCompletedDecimalEvidenceSurvivesEveryAdaptorBoundary(t *testing.T) {
	for _, mode := range []string{"poll", "immediate", "batch"} {
		t.Run(mode, func(t *testing.T) {
			plugin, err := pluginruntime.CompilePlugin(exactCompletionPlugin, pluginruntime.Options{})
			require.NoError(t, err)
			adaptor := New(plugin)
			body := `{"units":0.700000000000000001}`
			var result *relaycommon.TaskInfo
			switch mode {
			case "poll":
				result, err = adaptor.ParseTaskResultForTask(&model.Task{}, nil, []byte(body))
				require.NoError(t, err)
			case "batch":
				results, err := adaptor.ParseBatchResult([]*model.Task{{PrivateData: model.TaskPrivateData{UpstreamTaskID: "job"}}}, nil, []byte(`{"items":[{"id":"job","data":`+body+`}]} `))
				require.NoError(t, err)
				result = &results["job"].TaskInfo
			case "immediate":
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
				adaptor.Init(info)
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/submit", nil)
				response, taskErr := adaptor.ParseResponse(ctx, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, info)
				require.Nil(t, taskErr)
				result = response.Immediate
			}
			require.NotNil(t, result)
			require.Empty(t, result.UsageError)
			assert.Equal(t, "0.700000000000000001", result.UsageFacts["units"])
			assert.Zero(t, result.CompletionTokens, "credits never become token counters")
		})
	}
}

func TestBatchCannotBillRoundedProtocolDataWithoutSourcePath(t *testing.T) {
	source := strings.Replace(exactCompletionPlugin, `,usagePath:["items",index,"data"]`, "", 1)
	plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{})
	require.NoError(t, err)
	results, err := New(plugin).ParseBatchResult(nil, nil, []byte(`{"items":[{"id":"job","data":{"units":0.700000000000000001}}]}`))
	require.NoError(t, err)
	require.Contains(t, results, "job")
	assert.Equal(t, "SUCCESS", results["job"].TaskInfo.Status)
	assert.NotEmpty(t, results["job"].TaskInfo.UsageError)
	assert.Nil(t, results["job"].TaskInfo.UsageFacts)
}
