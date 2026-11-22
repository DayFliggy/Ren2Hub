package openai

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestoredProbeOaiResponsesCompactionKeepsExplicitCacheCreationInputTokens(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"cmp_1","object":"response.compaction","usage":{"input_tokens":100,"input_tokens_details":{"cache_creation_input_tokens":40}}}`)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
	usage, apiErr := OaiResponsesCompactionHandler(c, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 40, usage.PromptTokensDetails.CacheCreationTokensTotal())
}
