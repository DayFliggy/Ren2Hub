package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRetiredFrontendAPIRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	routes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	_, hasAsyncCleanup := routes[http.MethodPost+" /api/system-task/log-cleanup"]
	_, hasDirectDelete := routes[http.MethodDelete+" /api/log/"]
	_, hasConsoleMigration := routes[http.MethodPost+" /api/option/migrate_console_setting"]
	assert.True(t, hasAsyncCleanup)
	assert.False(t, hasDirectDelete)
	assert.False(t, hasConsoleMigration)
}

func TestRetiredPlanAPIsReturnNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/subscription/plans"},
		{http.MethodGet, "/api/subscription/self"},
		{http.MethodPut, "/api/subscription/self/preference"},
		{http.MethodPost, "/api/subscription/balance/pay"},
		{http.MethodPost, "/api/subscription/stripe/pay"},
		{http.MethodPost, "/api/subscription/epay/notify"},
		{http.MethodGet, "/api/subscription/admin/plans"},
		{http.MethodPost, "/api/subscription/admin/bind"},
		{http.MethodPost, "/api/option/waffo-pancake/subscription-product"},
	} {
		t.Run(endpoint.method+endpoint.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
			assert.Equal(t, http.StatusNotFound, response.Code)
		})
	}
}
