package claude

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestClaudeToGeminiStreamObservesFirstOutput(t *testing.T) {
	const channelID = 82101
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/claude-test:streamGenerateContent", nil)
	common.SetContextKey(c, constant.ContextKeyChannelId, channelID)
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"},
		OriginModelName: "claude-test",
		IsStream:        true,
		RelayFormat:     types.RelayFormatGemini,
	}
	service.BeginRouteObservation(c)
	service.MarkRouteAttemptSent(c, info)

	claudeInfo := &ClaudeResponseInfo{Usage: &dto.Usage{}}
	for _, event := range []string{
		`{"type":"message_start","message":{"id":"msg_1","model":"claude-test","usage":{"input_tokens":2,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"answer"}}`,
	} {
		require.Nil(t, HandleStreamResponseData(c, info, claudeInfo, event))
	}
	require.Contains(t, recorder.Body.String(), `"text":"answer"`)

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
