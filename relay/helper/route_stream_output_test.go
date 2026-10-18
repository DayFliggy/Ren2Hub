package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSSEHelpersMeasureEffectiveOutputInsteadOfControlFrames(t *testing.T) {
	tests := []struct {
		name, control, output string
		send                  func(*gin.Context, string) error
	}{
		{"chat", `{"choices":[{"delta":{"role":"assistant"}}]}`, `{"choices":[{"delta":{"content":"answer"}}]}`, StringData},
		{"gemini", `{"usageMetadata":{"promptTokenCount":10}}`, `{"candidates":[{"content":{"parts":[{"text":"answer"}]}}]}`, StringData},
		{"responses", `{"type":"response.created"}`, `{"type":"response.output_text.delta","delta":"answer"}`, func(c *gin.Context, data string) error {
			return ResponseChunkData(c, dto.ResponsesStreamResponse{}, data)
		}},
		{"claude", `{"type":"message_start"}`, `{"type":"content_block_delta","delta":{"text":"answer"}}`, func(c *gin.Context, data string) error {
			ClaudeChunkData(c, dto.ClaudeResponse{}, data)
			return nil
		}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			id := 81000 + index
			common.SetContextKey(c, constant.ContextKeyChannelId, id)
			info := &relaycommon.RelayInfo{OriginModelName: test.name, RelayMode: 1, IsStream: true}
			service.BeginRouteObservation(c)
			service.MarkRouteAttemptSent(c, info)
			info.SetFirstResponseTime()
			require.NoError(t, test.send(c, test.control))
			time.Sleep(25 * time.Millisecond)
			require.NoError(t, test.send(c, test.output))
			service.FinishRouteAttempt(c, info, nil)
			for _, row := range service.RoutingSnapshot().Observations {
				if row.ChannelID == id {
					require.Equal(t, 1, row.LatencySamples)
					require.GreaterOrEqual(t, row.LatencyMS, float64(20))
					return
				}
			}
			t.Fatal("successful SSE output did not reach route observations")
		})
	}
}
