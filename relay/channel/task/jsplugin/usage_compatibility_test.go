package jsplugin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	pluginruntime "github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompatibilityFactsAreValidatedWithoutInventingMissingValues(t *testing.T) {
	plugin, err := pluginruntime.NewRegistry().Register(`
export const meta={apiVersion:1,key:"compat-usage",name:"Compat",version:"1.0.0",author:{name:"Test"},models:["image"],fetchMode:"per_task",
usageProfiles:[{models:["image"],schema:{images:{type:"number",unit:"count"}},compatibilitySchema:{old_images:{type:"number",unit:"count"},old_mode:{enum:["none","video"]}}}]};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit",body:ctx.requestBody};}
export function parseSubmitResponse(){return {taskId:"task"};}
export function buildQueryRequest(){return {url:"https://provider.example/task"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
export function extractUsage(ctx){return ctx.requestBody.facts;}
`, pluginruntime.Options{})
	require.NoError(t, err)
	for _, test := range []struct {
		name  string
		facts map[string]any
		want  map[string]any
	}{
		{"declared compatibility", map[string]any{"images": 1, "old_images": 0, "old_mode": "none"}, map[string]any{"images": "1", "old_images": "0", "old_mode": "none"}},
		{"missing stays absent", map[string]any{"images": 1}, map[string]any{"images": "1"}},
		{"undeclared field", map[string]any{"images": 1, "hidden": 1}, nil},
		{"invalid compatibility count", map[string]any{"old_images": 129}, nil},
		{"invalid compatibility enum", map[string]any{"old_mode": "unknown"}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{OriginModelName: "image", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "ep-mapped", ChannelBaseUrl: "https://provider.example"}, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
			adaptor := New(plugin)
			adaptor.Init(info)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/images", nil)
			c.Set("task_request", map[string]any{"facts": test.facts})
			facts, err := adaptor.ExtractUsageFactsValidated(c, info)
			if test.want == nil {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, facts)
		})
	}
}
