//go:build known_upstream_restored

package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRestoredProbeRequestLoggerRedactsSensitiveQueryParameters(t *testing.T) {
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
	assert.False(t, strings.Contains(logged, "signature-secret"), "restored_request_logger:signature_secret_absent")
	assert.False(t, strings.Contains(logged, "credential-secret"), "restored_request_logger:credential_secret_absent")
	assert.False(t, strings.Contains(logged, "oauth-code"), "restored_request_logger:oauth_code_absent")
	assert.False(t, strings.Contains(logged, "oauth-state"), "restored_request_logger:oauth_state_absent")
	assert.True(t, strings.Contains(logged, "view=full"), "restored_request_logger:ordinary_query_preserved")
	assert.True(t, strings.Contains(logged, "X-Amz-Signature=%2A%2A%2Amasked%2A%2A%2A"), "restored_request_logger:signature_mask_present")
}
