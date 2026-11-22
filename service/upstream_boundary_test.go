package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpstreamResponseDropsCredentialsAndConnectionHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Header(common.RequestIdKey, "local-id")
	headers := http.Header{"Content-Type": {"application/json"}, "X-Trace": {"allowed"}, "Connection": {"keep-alive, X-Internal"}, "X-Internal": {"secret"}, common.RequestIdKey: {"upstream-id"}}
	blocked := []string{"Set-Cookie", "Cookie", "Authorization", "Proxy-Authorization", "Proxy-Authenticate", "Proxy-Connection", "X-Api-Key", "X-Goog-Api-Key", "Sec-WebSocket-Protocol", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}
	for _, name := range blocked {
		headers[name] = []string{"secret"}
	}
	IOCopyBytesGracefully(c, &http.Response{StatusCode: 200, Header: headers}, []byte(`{}`))
	for _, name := range append(blocked, "Connection", "X-Internal") {
		assert.Empty(t, w.Header().Get(name), name)
	}
	assert.Equal(t, "allowed", w.Header().Get("X-Trace"))
	assert.Equal(t, "2", w.Header().Get("Content-Length"))
	assert.Equal(t, "local-id", w.Header().Get(common.RequestIdKey))
	assert.Equal(t, "upstream-id", c.GetString(common.UpstreamRequestIdKey))
}

func TestErrorWrappersUseTheSameSanitizedProjection(t *testing.T) {
	cause := errors.New("api_key:WRAPPER_SECRET")
	claude := ClaudeErrorWrapper(cause, "failure", 502)
	assert.Equal(t, "api_key:***", claude.Error.Message)
	task := TaskErrorWrapper(cause, "failure", 502)
	assert.Equal(t, "api_key:***", task.Message)
	apiErr := types.NewError(cause, types.ErrorCodeDoRequestFailed)
	task = TaskErrorFromAPIError(apiErr)
	assert.Equal(t, "api_key:***", task.Message)
	assert.ErrorIs(t, task.Error, cause)
}

type brokenUpstreamBody struct{ closed bool }

func (*brokenUpstreamBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (b *brokenUpstreamBody) Close() error           { b.closed = true; return nil }

func TestRelayErrorReadFailureClosesBodyAndKeepsCause(t *testing.T) {
	body := &brokenUpstreamBody{}
	err := RelayErrorHandler(context.Background(), &http.Response{StatusCode: 502, Body: body}, false)
	assert.True(t, body.closed)
	assert.True(t, errors.Is(err, io.ErrUnexpectedEOF), "read error must remain diagnosable")
}

func TestRelayErrorSanitizesBeforeLoggingAndReturning(t *testing.T) {
	var output bytes.Buffer
	common.LogWriterMu.Lock()
	old := gin.DefaultErrorWriter
	gin.DefaultErrorWriter = &output
	common.LogWriterMu.Unlock()
	t.Cleanup(func() { common.LogWriterMu.Lock(); gin.DefaultErrorWriter = old; common.LogWriterMu.Unlock() })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyChannelKey, "FAKE_CHANNEL_SECRET")
	common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, "https://provider.internal.invalid")
	for _, show := range []bool{false, true} {
		for _, body := range []string{`bad response FAKE_CHANNEL_SECRET https://provider.internal.invalid/private`, `{"error":{"message":"FAKE_CHANNEL_SECRET","type":"server_error","metadata":{"api_key":"OTHER_SECRET","nested":[{"token":"NESTED_SECRET","retry":2}]}}}`} {
			err := RelayErrorHandler(c, &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader(body))}, show)
			require.NotNil(t, err)
			for _, secret := range []string{"FAKE_CHANNEL_SECRET", "OTHER_SECRET", "NESTED_SECRET", "provider.internal.invalid"} {
				assert.NotContains(t, output.String()+err.Error()+err.ToOpenAIError().Message, secret)
			}
		}
	}
}
