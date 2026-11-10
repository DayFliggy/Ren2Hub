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
	for _, path := range []string{"/market", "/market/example", "/console/market", "/pg", "/pg/chat/completions", "/playground", "/next/keys", "/subscription", "/plan-management", "/deployments", "/console/chat", "/console/playground", "/api/subscription/plans", "/api/subscription/admin/plans", "/api/deployments", "/api/deployments/settings", "/api/system-info/instances", "/api/group", "/api/group/1", "/api/user/groups", "/api/user/self/groups", "/api/token/auto-groups", "/api/option/rest_model_ratio", "/api/codex/oauth/start", "/api/channel/7/codex/usage", "/api/channel/7/codex/usage/reset-credits", "/api/channel/7/codex/refresh", "/api/option/waffo-pancake/subscription-product", "/api/option/waffo-pancake/subscription-product-options"} {
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
	// Host namespaces that Ren2Hub still serves remain open to plugin routes.
	for _, path := range []string{"/api/groups-report", "/api/subscriptions", "/v1/systemone", "/api/channel/7/codexes", "/api/option/waffo-pancake/catalog-extra"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusNoContent, recorder.Code, path)
	}
}
