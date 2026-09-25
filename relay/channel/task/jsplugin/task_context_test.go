package jsplugin

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/model"
	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskPollingHooksHonorCancellation(t *testing.T) {
	for _, hook := range []string{"buildQueryRequest", "parseTaskResult", "buildBatchQueryRequest", "parseBatchResult", "extractUsageOnComplete"} {
		t.Run(hook, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := `
export const meta = {apiVersion:1,key:"poll-cancel",name:"Cancellation",version:"1.0.0",author:{name:"Test"},models:["m"],fetchMode:"per_task"};
export function buildSubmitRequest(){return {url:"https://example.com"};}
export function parseSubmitResponse(){return {taskId:"job"};}
export function buildQueryRequest(){return {url:"https://example.com"};}
export function parseTaskResult(){return {taskId:"job",status:"SUCCESS"};}
export function buildBatchQueryRequest(){return {url:"https://example.com"};}
export function parseBatchResult(){return [{taskId:"job",status:"SUCCESS"}];}
export function extractUsageOnComplete(){return {upstreamUnits:1};}
`
			source += fmt.Sprintf("\n%s = function(){console.log('cancel'); while (true) {}};", hook)
			plugin, err := pluginruntime.CompilePlugin(source, pluginruntime.Options{Log: func(string) { cancel() }})
			require.NoError(t, err)
			adaptor := New(plugin)
			adaptor.SetTaskContext(ctx)
			task := &model.Task{TaskID: "job"}
			switch hook {
			case "buildQueryRequest":
				_, err = adaptor.FetchTaskForTask("https://example.com", "", task, "")
			case "parseTaskResult":
				_, err = adaptor.ParseTaskResultForTask(task, nil, []byte(`{}`))
			case "buildBatchQueryRequest":
				_, err = adaptor.FetchBatchTasks("https://example.com", "", []*model.Task{task}, "")
			case "parseBatchResult":
				_, err = adaptor.ParseBatchResult([]*model.Task{task}, nil, []byte(`{}`))
			case "extractUsageOnComplete":
				result, parseErr := adaptor.ParseTaskResultForTask(task, nil, []byte(`{}`))
				require.NoError(t, parseErr)
				require.NotNil(t, result)
				assert.NotEmpty(t, result.UsageError, "cancelled usage cannot be settled as complete")
				assert.Empty(t, result.UsageFacts)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "context canceled")
		})
	}
}
