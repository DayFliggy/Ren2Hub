package openai

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHandlerCommitsCurrentTextFrameBeforeIdleTimeout(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 1
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	reader, writer := io.Pipe()
	t.Cleanup(func() {
		_ = reader.Close()
		_ = writer.Close()
	})
	go func() {
		_, _ = fmt.Fprint(writer,
			"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n"+
				"data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n")
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set(common.RequestIdKey, "stream-commit-test")
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-test"},
		RelayMode:   relayconstant.RelayModeChatCompletions,
		RelayFormat: types.RelayFormatOpenAI,
		IsStream:    true,
		DisablePing: true,
	}
	info.SetEstimatePromptTokens(1)
	resp := &http.Response{StatusCode: http.StatusOK, Body: reader, Header: http.Header{"Content-Type": []string{"text/event-stream"}}}

	usage, apiErr := OaiStreamHandler(c, info, resp)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	require.NotNil(t, info.StreamStatus)
	assert.True(t, info.HasValidOutput())
	assert.Contains(t, recorder.Body.String(), `"role":"assistant"`)
	assert.Contains(t, recorder.Body.String(), `"content":"hello"`)
	assert.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestOpenaiTTSStreamDoesNotCommitHeadersWithoutContent(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	c.Set(common.RequestIdKey, "tts-stream-test")
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "tts-test"},
		IsStream:    true,
		DisablePing: true,
	}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\ndata: [DONE]\n")),
		Header: http.Header{
			"Content-Type":          []string{"audio/mpeg"},
			"X-Upstream-Request-Id": []string{"upstream-1"},
		},
	}

	usage, apiErr := OpenaiTTSHandler(c, resp, info)

	require.Nil(t, usage)
	require.NotNil(t, apiErr)
	assert.False(t, c.Writer.Written())
	assert.Empty(t, recorder.Body.String())
	assert.Empty(t, recorder.Header().Get("Content-Type"))
	assert.Empty(t, recorder.Header().Get("X-Upstream-Request-Id"))
}
