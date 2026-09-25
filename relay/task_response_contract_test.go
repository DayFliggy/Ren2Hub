package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskResponseAdaptersDeferClientCommit(t *testing.T) {
	for _, test := range []struct {
		name     string
		platform constant.TaskPlatform
		body     string
	}{
		{"sora", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeSora)), `{"id":"provider-task","status":"queued"}`},
		{"ali", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeAli)), `{"output":{"task_id":"provider-task","task_status":"PENDING"}}`},
		{"doubao", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeDoubaoVideo)), `{"id":"provider-task"}`},
		{"gemini", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeGemini)), `{"name":"models/test/operations/provider-task"}`},
		{"vertex", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeVertexAi)), `{"name":"projects/test/operations/provider-task"}`},
		{"hailuo", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeMiniMax)), `{"task_id":"provider-task","base_resp":{"status_code":0}}`},
		{"jimeng", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeJimeng)), `{"code":10000,"data":{"task_id":"provider-task"}}`},
		{"kling", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeKling)), `{"code":0,"data":{"task_id":"provider-task"}}`},
		{"vidu", constant.TaskPlatform(fmt.Sprint(constant.ChannelTypeVidu)), `{"task_id":"provider-task","state":"created"}`},
		{"suno", constant.TaskPlatformSuno, `{"code":"success","data":"provider-task"}`},
	} {
		for _, status := range []int{200, 201, 202} {
			t.Run(fmt.Sprintf("%s/%d", test.name, status), func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://example.com", UpstreamModelName: "wan2.6-t2v"}, OriginModelName: "wan2.6-t2v", TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
				key := test.name
				if key == "ali" {
					key = "alibaba"
				}
				if key == "gemini" {
					key = "google"
				}
				if key == "vertex" {
					key = "vertex-ai"
				}
				if key == "suno" {
					key = "sunoapi"
				}
				source, err := plugins.Source(key)
				require.NoError(t, err)
				loaded, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
				require.NoError(t, err)
				adaptor := taskplugin.New(loaded)
				adaptor.Init(info)
				response := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(test.body))}
				id, raw, client, taskErr := adaptor.DoResponse(c, response, info)
				require.Nil(t, taskErr)
				require.NotEmpty(t, id)
				assert.NotEmpty(t, raw, "plugin returns durable task data before presentation")
				assert.Nil(t, client, "public response is rendered only after durable acceptance")
				assert.False(t, c.Writer.Written(), "controller must persist the task before sending its public ID")
			})
		}
	}
}
