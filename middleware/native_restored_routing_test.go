//go:build known_upstream_restored

package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRestoredProbeGeminiUnknownAction(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-test:unknown", strings.NewReader(`{"contents":[{"parts":[{"text":"hi"}]}]}`))
	_, _, err := getModelRequest(c)
	assert.Error(t, err)
}
