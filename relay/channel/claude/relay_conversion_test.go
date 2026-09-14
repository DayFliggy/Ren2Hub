package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeProtocolConversionsReachDownstream(t *testing.T) {
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		for _, stream := range []bool{false, true} {
			t.Run(string(target)+map[bool]string{true: "/stream", false: "/json"}[stream], func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				info := &relaycommon.RelayInfo{RelayFormat: target, IsStream: stream, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
				var usage *dto.Usage
				var apiErr *types.NewAPIError
				body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":2,"output_tokens":3}}`
				if stream {
					body = "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\n" +
						"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
						"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n" +
						"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
						"data: {\"type\":\"message_stop\"}\n\n"
				}
				resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
				if stream {
					usage, apiErr = ClaudeStreamHandler(c, resp, info)
				} else {
					usage, apiErr = ClaudeHandler(c, resp, info)
				}
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, 2, usage.PromptTokens)
				assert.Equal(t, 3, usage.CompletionTokens)
				assert.Contains(t, recorder.Body.String(), "hello")
				if target == types.RelayFormatGemini {
					assert.Contains(t, recorder.Body.String(), "candidates")
				} else {
					assert.Contains(t, recorder.Body.String(), "output_text")
				}
			})
		}
	}
}
