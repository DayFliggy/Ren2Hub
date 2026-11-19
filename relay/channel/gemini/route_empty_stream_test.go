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
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A native Gemini stream whose only parts are code execution served the
// request: executableCode and codeExecutionResult are model output.
func TestRouteEvidenceCountsGeminiCodeExecution(t *testing.T) {
	ev := runGeminiRouteStream(t, types.RelayFormatGemini, gemini.GeminiTextGenerationStreamHandler,
		`{"candidates":[{"content":{"role":"model","parts":[{"executableCode":{"language":"PYTHON","code":"print(1)"}}]},"index":0}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"codeExecutionResult":{"outcome":"OUTCOME_OK","output":"1\n"}}]},"finishReason":"STOP","index":0}]}`)
	require.Equal(t, "success", ev.Get("outcome").String())
	require.Empty(t, ev.Get("outcome_reason").String())
}

// A file part is output in the raw Gemini frame. Converted to chat the file
// is lost and only the stop remains, so the raw payload keeps the attempt
// from counting as an empty stream.
func TestRouteEvidenceGeminiFileIsNotEmptyStream(t *testing.T) {
	frame := `{"candidates":[{"content":{"role":"model","parts":[{"fileData":{"mimeType":"application/pdf","fileUri":"gs://bucket/report.pdf"}}]},"finishReason":"STOP","index":0}]}`
	for _, tc := range []struct {
		name    string
		format  types.RelayFormat
		handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"direct", types.RelayFormatGemini, gemini.GeminiTextGenerationStreamHandler},
		{"to chat", types.RelayFormatOpenAI, gemini.GeminiChatStreamHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := runGeminiRouteStream(t, tc.format, tc.handler, frame)
			require.NotEqual(t, "failure", ev.Get("outcome").String(), ev.Raw)
		})
	}
}

func runGeminiRouteStream(t *testing.T, format types.RelayFormat, handler func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError), frames ...string) gjson.Result {
	t.Helper()
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
		IsStream: true, RelayFormat: format, DisablePing: true}
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body.String()))}
	service.MarkRouteAttemptSent(c, info)
	_, apiErr := handler(c, info, resp)
	require.Nil(t, apiErr)
	service.FinishRouteAttempt(c, info, apiErr)
	events, err := common.Marshal(service.RequestPolicy(c).Events())
	require.NoError(t, err)
	return gjson.GetBytes(events, "0.route_attempt")
}
