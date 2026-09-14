package coze

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCozeStreamRequiresExplicitCompletion(t *testing.T) {
	const delta = "event: conversation.message.delta\ndata: {\"content\":\"hello\",\"type\":\"answer\"}\n\n"
	const complete = "event: conversation.chat.completed\ndata: {\"usage\":{\"input_count\":10,\"output_count\":2,\"token_count\":12}}\n\n"
	const prelude = "event: conversation.chat.created\ndata: {}\n\n"
	for _, tc := range []struct {
		name, data    string
		ok, delivered bool
	}{
		{"completed", prelude + delta + delta + complete, true, true},
		{"truncated", delta, false, true},
		{"malformed", delta + "event: conversation.message.delta\ndata: {\n\n", false, true},
		{"provider_error", delta + "event: error\ndata: {\"code\":123}\n\n", false, true},
		{"prelude_limit", strings.Repeat(prelude, 65), false, false},
		{"prelude_eof", prelude, false, false},
		{"foreign_terminal", delta + "data: [DONE]\n\n", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, IsStream: true, StartTime: time.Now(), ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o-mini"}}
			usage, err := cozeChatStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(tc.data))})
			assert.Equal(t, tc.delivered, info.HasValidOutput())
			if tc.ok {
				require.Nil(t, err)
				require.NotNil(t, usage)
				assert.Equal(t, 12, usage.TotalTokens)
				assert.Equal(t, "provider", usage.UsageSource)
				assert.Equal(t, 1, strings.Count(recorder.Body.String(), "[DONE]"))
				assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"role":"assistant"`))
				assert.Equal(t, 2, strings.Count(recorder.Body.String(), `"content":"hello"`))
				assert.NotContains(t, recorder.Body.String(), `"index":1`)
				assert.True(t, info.StreamStatus.IsNormalEnd())
				return
			}
			require.NotNil(t, err)
			assert.False(t, info.StreamStatus.IsNormalEnd())
			assert.NotContains(t, recorder.Body.String(), "[DONE]")
			assert.NotContains(t, recorder.Body.String(), `"finish_reason":"stop"`)
			if tc.delivered {
				require.NotNil(t, usage)
				assert.NotEqual(t, "provider", usage.UsageSource)
			} else {
				assert.False(t, c.Writer.Written())
			}
		})
	}
}
