package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestPaymentNotificationRoutesAreRegistered pins the callback surface both
// payment rails expose. The routes are anonymous by design, so a missing
// registration would silently turn every gateway notification into a 404 and
// leave orders unsettled.
func TestPaymentNotificationRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)

	routes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}

	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, "/api/user/gmpay/notify"},
		{http.MethodGet, "/api/user/gmpay/notify"},
		{http.MethodPost, "/api/user/epay/notify"},
		{http.MethodGet, "/api/user/epay/notify"},
	} {
		_, registered := routes[endpoint.method+" "+endpoint.path]
		assert.True(t, registered, "%s %s must be registered", endpoint.method, endpoint.path)
	}

	// A registered route must reject an unauthenticated body it cannot verify
	// with a client error rather than a routing 404.
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/user/gmpay/notify", nil))
	assert.NotEqual(t, http.StatusNotFound, response.Code)
	assert.NotEqual(t, http.StatusMethodNotAllowed, response.Code)
}
