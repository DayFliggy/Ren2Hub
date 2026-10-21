package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// E2-6：一次扫描取出 model、stream、stream_format；只认顶层字段，请求体仍可被后续读取。
// 路由观测按 stream 分桶（B7），这里的解析结果决定样本落在哪一桶。
func TestModelFromJSONBodyReadsTopLevelStreamFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		body, model, format string
		stream              bool
		err                 string
	}{
		{body: `{"model":"m","stream":true}`, model: "m", stream: true},
		{body: `{"stream_format":"sse","model":"m"}`, model: "m", format: "sse"},
		{body: `{"model":"m","metadata":{"stream":true,"stream_format":"sse"}}`, model: "m"},
		{body: `{"stream":true}`, stream: true},
		{body: `{"model":7,"stream":true}`, err: "field model must be a string"},
		{body: `{"model":"a","model":"b"}`, err: "model must be provided once"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		got, err := getModelFromRequest(c)
		if tc.err != "" {
			require.EqualError(t, err, tc.err, tc.body)
			continue
		}
		require.NoError(t, err, tc.body)
		require.Equal(t, ModelRequest{Model: tc.model, Stream: tc.stream, StreamFormat: tc.format}, *got, tc.body)
		replay, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Equal(t, tc.body, string(replay), "the body stays readable for the relay")
	}
}
