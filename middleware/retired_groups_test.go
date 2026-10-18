package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRetiredGroupsRejectExplicitOldRestrictionsAndPreserveBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyStorageCleanup(), RejectRetiredGroups())
	called := 0
	r.POST("/api/channel/", func(c *gin.Context) {
		var body map[string]any
		require.NoError(t, c.ShouldBindJSON(&body))
		called++
		common.ApiSuccess(c, body)
	})
	for _, body := range []string{`{"group":""}`, `{"channel":{"group":"default"}}`, `{"auto_groups":[]}`, `{"cross_group_retry":false}`} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/channel/", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, 400, w.Code)
	}
	require.Zero(t, called)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/channel/", strings.NewReader(`{"channel":{"name":"kept","channel_ratio":"1"}}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	require.Equal(t, 1, called)
	require.Contains(t, w.Body.String(), "kept")
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/channel/?group=default", strings.NewReader(`{}`))
	r.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
	require.Equal(t, 1, called)
}
