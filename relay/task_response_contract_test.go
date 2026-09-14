package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
				info := &relaycommon.RelayInfo{OriginModelName: "task-model", TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"}}
				adaptor := GetTaskAdaptor(test.platform)
				require.NotNil(t, adaptor)
				response := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(test.body))}
				id, raw, client, taskErr := adaptor.DoResponse(c, response, info)
				require.Nil(t, taskErr)
				require.NotEmpty(t, id)
				assert.JSONEq(t, test.body, string(raw))
				encoded, err := common.Marshal(client)
				require.NoError(t, err)
				assert.Contains(t, string(encoded), "task_public")
				assert.NotContains(t, string(encoded), "provider-task")
				assert.False(t, c.Writer.Written(), "controller must persist the task before sending its public ID")
			})
		}
	}
}
