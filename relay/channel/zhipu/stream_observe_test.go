package zhipu

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// gin 的 c.Stream 需要 http.CloseNotifier；nil 通道表示客户端一直在线。
type streamRecorder struct{ *httptest.ResponseRecorder }

func (streamRecorder) CloseNotify() <-chan bool { return nil }

func TestZhipuStreamObservesFirstOutput(t *testing.T) {
	const channelID = 82401
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(streamRecorder{recorder})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	info := &relaycommon.RelayInfo{OriginModelName: "chatglm-test", IsStream: true}
	service.BeginRouteObservation(c)
	service.MarkRouteAttemptSent(c, info)

	body := strings.Join([]string{
		`data:answer`,
		`meta:{"request_id":"req_1","task_id":"task_1","task_status":"SUCCESS","usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`,
	}, "\n")
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	usage, apiErr := zhipuStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Contains(t, recorder.Body.String(), `"content":"answer"`)

	service.FinishRouteAttempt(c, info, nil)
	for _, row := range service.RoutingSnapshot().Observations {
		if row.ChannelID == channelID {
			require.Equal(t, 1, row.Samples)
			require.Equal(t, 1, row.LatencySamples, "a directly written stream output must end the latency sample")
			return
		}
	}
	t.Fatal("stream attempt did not reach route observations")
}
