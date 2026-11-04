package xai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXAIStreamUsagePreservesBillingDetails(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	payload := `{"id":"response","model":"grok-test","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":30,"total_tokens":130,"prompt_tokens_details":{"cached_tokens":40},"completion_tokens_details":{"reasoning_tokens":10,"text_tokens":20}}}`
	newContext := func() (*gin.Context, *relaycommon.RelayInfo) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-test"}}
		info.SetEstimatePromptTokens(999)
		return ctx, info
	}
	ctx, info := newContext()
	usage, apiErr := xAIStreamHandler(ctx, info, &http.Response{Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n"))})
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 100, usage.PromptTokens)
	assert.Equal(t, 30, usage.CompletionTokens)
	assert.Equal(t, 40, usage.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 10, usage.CompletionTokenDetails.ReasoningTokens)
	assert.Equal(t, 20, usage.CompletionTokenDetails.TextTokens)
	ctx, info = newContext()
	nonstream, apiErr := xAIHandler(ctx, info, &http.Response{Body: io.NopCloser(strings.NewReader(payload))})
	require.Nil(t, apiErr)
	assert.Equal(t, nonstream, usage, "stream and nonstream must feed the same billing facts")
}

func TestXAIStreamMissingUsageKeepsEstimate(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "grok-test"}}
	info.SetEstimatePromptTokens(17)
	usage, apiErr := xAIStreamHandler(ctx, info, &http.Response{Body: io.NopCloser(strings.NewReader("data: {\"choices\":[]}\n\ndata: [DONE]\n\n"))})
	require.Nil(t, apiErr)
	assert.Equal(t, 17, usage.PromptTokens)
	assert.Zero(t, usage.CompletionTokens)
}
