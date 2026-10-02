package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestGatewayAdmissionRejectsInvalidCorrelationBeforeParsingOrSending(t *testing.T) {
	for _, value := range []string{"", "with space", strings.Repeat("a", 129)} {
		router := gin.New()
		called := false
		router.POST("/v1/chat/completions", GatewayAdmission(), func(c *gin.Context) { called = true; c.Status(204) })
		request := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`invalid json`))
		request.Header.Set("X-Ren2Hub-Trace-Id", value)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.False(t, called)
		assert.Contains(t, response.Body.String(), "invalid request correlation identifier")
	}
}
