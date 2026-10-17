package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRetiredRoutesRejectBeforePluginDispatch(t *testing.T) {
	router := gin.New()
	calls := 0
	SetWebRouter(router, WebAssets{}, func(c *gin.Context) {
		calls++
		c.AbortWithStatus(http.StatusNoContent)
	})
	for _, path := range []string{"/pg", "/pg/chat/completions", "/playground", "/next/keys", "/subscription", "/plan-management", "/deployments", "/console/chat", "/console/playground"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
			require.Equal(t, http.StatusNotFound, recorder.Code, "%s %s", method, path)
		}
	}
	require.Zero(t, calls, "a plugin cannot claim a retired product namespace")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/pg-task", nil))
	require.Equal(t, http.StatusNoContent, recorder.Code)
	require.Equal(t, 1, calls, "namespace matching must use path segments")
}
