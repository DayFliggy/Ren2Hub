package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRequestLoggerRedactsSensitiveQueryParameters(t *testing.T) {
	oldMode, oldWriter := gin.Mode(), gin.DefaultWriter
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		gin.SetMode(oldMode)
		gin.DefaultWriter = oldWriter
	})

	var output bytes.Buffer
	gin.DefaultWriter = &output
	server := gin.New()
	SetUpLogger(server)
	server.GET("/signed", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	server.GET("/api/oauth/callback", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet,
		"/signed?X-Amz-Signature=signature-secret&X-Goog-Credential=credential-secret&view=full", nil))
	server.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet,
		"/api/oauth/callback?code=oauth-code&state=oauth-state", nil))

	logged := output.String()
	assert.NotContains(t, logged, "signature-secret")
	assert.NotContains(t, logged, "credential-secret")
	assert.NotContains(t, logged, "oauth-code")
	assert.NotContains(t, logged, "oauth-state")
	assert.Contains(t, logged, "view=full")
	assert.Contains(t, logged, "X-Amz-Signature=%2A%2A%2Amasked%2A%2A%2A")
}
