package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloneRequestHeadersCombinesRepeatedLines(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Add("anthropic-beta", "context-1m-2025-08-07, ")
	c.Request.Header.Add("anthropic-beta", " fast-mode-2026-02-01,context-1m-2025-08-07")
	c.Request.Header.Add("X-Tier", "gold")
	c.Request.Header.Add("X-Tier", "fast")
	c.Request.Header.Add("Cookie", "a=1")
	c.Request.Header.Add("Cookie", "b=2")
	c.Request.Header.Set("Authorization", "Bearer sk-test")

	headers := cloneRequestHeaders(c)
	require.NotNil(t, headers)
	assert.Equal(t, "context-1m-2025-08-07,fast-mode-2026-02-01", headers["Anthropic-Beta"])
	assert.Equal(t, "gold,fast", headers["X-Tier"])
	assert.Equal(t, "a=1; b=2", headers["Cookie"])
	assert.Equal(t, "Bearer sk-test", headers["Authorization"])
}
