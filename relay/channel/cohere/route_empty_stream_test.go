package cohere

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Cohere's handler gives the route observer only its converted chat frames,
// not the raw upstream frames with the billed output. A completion it reports
// without text is therefore not proof of an empty response: the COMPLETE
// frame here bills five output tokens.
func TestRouteEvidenceCohereCompletionWithoutTextIsNotEmptyStream(t *testing.T) {
	c, _ := gin.CreateTestContext(streamRecorder{httptest.NewRecorder()})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("route_observation_epoch", uint64(0))
	common.SetContextKey(c, constant.ContextKeyChannelId, 83003)
	service.RequestPolicy(c).BeginAttempt(&model.Channel{Id: 83003})
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "command-r"}, OriginModelName: "command-r",
		IsStream: true, RelayFormat: types.RelayFormatOpenAI}
	body := strings.Join([]string{
		`{"is_finished":false,"event_type":"stream-start"}`,
		`{"is_finished":true,"event_type":"stream-end","finish_reason":"COMPLETE","response":{"response_id":"r1","text":"","meta":{"billed_units":{"input_tokens":3,"output_tokens":5}}}}`,
	}, "\n") + "\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/stream+json"}}, Body: io.NopCloser(strings.NewReader(body))}
	service.MarkRouteAttemptSent(c, info)
	usage, apiErr := cohereStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.Equal(t, 5, usage.CompletionTokens, "the handler bills the reported output")
	service.FinishRouteAttempt(c, info, apiErr)
	events, err := common.Marshal(service.RequestPolicy(c).Events())
	require.NoError(t, err)
	ev := gjson.GetBytes(events, "0.route_attempt")
	require.Equal(t, "ignored", ev.Get("outcome").String())
	require.Equal(t, "no_visible_output", ev.Get("outcome_reason").String())
}
