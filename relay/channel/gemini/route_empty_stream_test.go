package gemini_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A native Gemini stream whose only parts are code execution served the
// request: executableCode and codeExecutionResult are model output.
func TestRouteEvidenceCountsGeminiCodeExecution(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/empty-test:streamGenerateContent?alt=sse", nil)
	c.Set("route_observation_epoch", uint64(0))
	common.SetContextKey(c, constant.ContextKeyChannelId, 83002)
	service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 83002})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "empty-test"}, OriginModelName: "empty-test",
		IsStream: true, RelayFormat: types.RelayFormatGemini, DisablePing: true}
	frames := []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"executableCode":{"language":"PYTHON","code":"print(1)"}}]},"index":0}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1\n"}}]},"finishReason":"STOP","index":0}]}`,
	}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}
	service.MarkRouteAttemptSent(c, info)
	_, apiErr := gemini.GeminiTextGenerationStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	service.FinishRouteAttempt(c, info, apiErr)
	events, err := common.Marshal(service.RequestPolicy(c).Events())
	require.NoError(t, err)
	ev := gjson.GetBytes(events, "0.route_attempt")
	require.Equal(t, "success", ev.Get("outcome").String())
	require.Empty(t, ev.Get("outcome_reason").String())
}
