package vertex

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexClaudeRequestUsesSelectedNativeProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		mode        int
	}{
		{"Gemini", "gemini-2.5-flash", RequestModeGemini},
		{"OpenSource", "llama-3", RequestModeOpenSource},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.model}}
			request := &dto.ClaudeRequest{Model: tc.model, MaxTokens: common.GetPointer[uint](32), Temperature: common.GetPointer[float64](0),
				Messages: []dto.ClaudeMessage{{Role: "user", Content: "hello"}}}
			result, err := (&Adaptor{RequestMode: tc.mode}).ConvertClaudeRequest(c, info, request)
			require.NoError(t, err)
			if tc.mode == RequestModeGemini {
				converted, ok := result.(*dto.GeminiChatRequest)
				require.True(t, ok)
				require.Len(t, converted.Contents, 1)
				assert.Equal(t, "hello", converted.Contents[0].Parts[0].Text)
				assert.EqualValues(t, 32, *converted.GenerationConfig.MaxOutputTokens)
				require.NotNil(t, converted.GenerationConfig.Temperature)
				assert.Zero(t, *converted.GenerationConfig.Temperature)
			} else {
				converted, ok := result.(*dto.GeneralOpenAIRequest)
				require.True(t, ok)
				require.Len(t, converted.Messages, 1)
				assert.Equal(t, "hello", converted.Messages[0].StringContent())
				require.NotNil(t, converted.Temperature)
				assert.Zero(t, *converted.Temperature)
			}
		})
	}
}
