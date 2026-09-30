package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestFetchUpstreamRatiosRejectsInvalidSourceRequests(t *testing.T) {
	router := gin.New()
	router.POST("/fetch", FetchUpstreamRatios)
	for _, body := range []string{`not json`, `{"sources":["unknown"]}`, `{"sources":["basellm","basellm"]}`, `{"sources":"basellm"}`, strings.Repeat("x", 16385)} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/fetch", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Contains(t, response.Body.String(), `"success":false`)
	}
}
