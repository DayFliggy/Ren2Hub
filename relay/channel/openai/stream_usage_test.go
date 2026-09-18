package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatStreamUsageFilteringPreservesChoices(t *testing.T) {
	for _, tc := range []struct {
		name         string
		choices      string
		includeUsage bool
		wantFrames   int
		wantPath     string
		wantValue    string
	}{
		{"finish_with_usage", `[{"index":0,"delta":{},"finish_reason":"stop"}]`, false, 2, "choices.0.finish_reason", "stop"},
		{"tool_with_usage", `[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":"tool_calls"}]`, false, 2, "choices.0.delta.tool_calls.0.id", "call_1"},
		{"text_with_usage", `[{"index":0,"delta":{"content":"tail"},"finish_reason":"stop","provider_field":false}]`, false, 2, "choices.0.provider_field", "false"},
		{"usage_only_excluded", `[]`, false, 1, "choices.0.delta.content", "body"},
		{"usage_only_requested", `[]`, true, 2, "usage.total_tokens", "12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, DisablePing: true, ShouldIncludeUsage: tc.includeUsage,
				ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test-model"}}
			body := "data: {\"id\":\"chat_1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"body\"},\"finish_reason\":null}]}\n\n"
			if tc.choices == "[]" {
				body = strings.Replace(body, "\"finish_reason\":null", "\"finish_reason\":\"stop\"", 1)
			}
			body += fmt.Sprintf("data: {\"id\":\"chat_1\",\"choices\":%s,\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\ndata: [DONE]\n\n", tc.choices)
			usage, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 12, usage.TotalTokens)
			var frames []string
			for _, line := range strings.Split(recorder.Body.String(), "\n") {
				if strings.HasPrefix(line, "data: {") {
					frames = append(frames, strings.TrimPrefix(line, "data: "))
				}
			}
			require.Len(t, frames, tc.wantFrames)
			assert.Equal(t, tc.wantValue, gjson.Get(frames[len(frames)-1], tc.wantPath).String())
			if !tc.includeUsage {
				for _, frame := range frames {
					assert.False(t, gjson.Get(frame, "usage").Exists(), frame)
				}
			}
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
		})
	}
}
