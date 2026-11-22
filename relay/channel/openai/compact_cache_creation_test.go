package openai

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompactPreservesCacheCreationAndResponseBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"id":"cmp_1","object":"response.compaction","usage":{"input_tokens":100,"input_tokens_details":{"cache_creation_input_tokens":40,"cache_write_tokens":40,"cached_tokens":10}}}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": []string{"application/json"}}}
	usage, err := OaiResponsesCompactionHandler(c, resp)
	require.Nil(t, err)
	require.Equal(t, 40, usage.PromptTokensDetails.CacheCreationInputTokens)
	require.Equal(t, 40, usage.PromptTokensDetails.CacheCreationTokensTotal())
	require.Equal(t, 10, usage.PromptTokensDetails.CachedTokens)
	require.Equal(t, body, recorder.Body.String())
}
