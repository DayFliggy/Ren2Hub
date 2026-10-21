package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func beginObservedGeminiStream(t *testing.T, channelID int) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gpt-test:streamGenerateContent", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		OriginModelName: "gpt-test",
		IsStream:        true,
		RelayFormat:     types.RelayFormatGemini,
		DisablePing:     true,
	}
	service.BeginRouteObservation(c)
	service.MarkRouteAttemptSent(c, info)
	return c, recorder, info
}

func requireFirstOutputSample(t *testing.T, c *gin.Context, info *relaycommon.RelayInfo, channelID int) {
	t.Helper()
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

func TestChatToGeminiStreamObservesFirstOutput(t *testing.T) {
	c, recorder, info := beginObservedGeminiStream(t, 82001)
	chunk := `{"id":"chatcmpl-1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-test","choices":[{"index":0,"delta":{"role":"assistant","content":"answer"}}]}`
	require.NoError(t, HandleStreamFormat(c, info, chunk, false, false))
	require.Contains(t, recorder.Body.String(), `"text":"answer"`)
	requireFirstOutputSample(t, c, info, 82001)
}

func TestResponsesToGeminiStreamObservesFirstOutput(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	c, recorder, info := beginObservedGeminiStream(t, 82002)
	body := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-test","created_at":1710000000}}`,
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
	}
	_, apiErr := OaiResponsesToChatStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.Contains(t, recorder.Body.String(), `"text":"answer"`)
	requireFirstOutputSample(t, c, info, 82002)
}
