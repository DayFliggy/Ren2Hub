package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionRefreshRoutePreservesOriginAndSeparateLoginBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedis, previousClient := common.RedisEnabled, common.RDB
	previousGlobal, previousCritical := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	previousSecure, previousOrigins := common.SessionCookieSecure, common.SessionCookieTrustedURLs
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	common.RedisEnabled, common.RDB = true, client
	common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = false, true
	common.SessionCookieSecure, common.SessionCookieTrustedURLs = true, nil
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousRedis, previousClient
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = previousGlobal, previousCritical
		common.SessionCookieSecure, common.SessionCookieTrustedURLs = previousSecure, previousOrigins
		assert.NoError(t, client.Close())
	})

	engine := gin.New()
	require.NoError(t, engine.SetTrustedProxies(nil))
	SetApiRouter(engine)
	request := func(path, origin string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "https://dashboard.example"+path, nil)
		request.RemoteAddr = "192.0.2.70:12345"
		request.Header.Set("Origin", origin)
		engine.ServeHTTP(response, request)
		return response
	}

	assert.Equal(t, http.StatusForbidden, request("/api/user/auth/refresh", "https://untrusted.example").Code)
	for i := 0; i < common.SessionRefreshRateLimitNum; i++ {
		response := request("/api/user/auth/refresh", "https://dashboard.example")
		require.Equal(t, http.StatusUnauthorized, response.Code, "refresh without a cookie stays unauthenticated")
		assert.Contains(t, response.Header().Get("Cache-Control"), "no-store")
	}
	limited := request("/api/user/auth/refresh", "https://dashboard.example")
	assert.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "60", limited.Header().Get("Retry-After"))
	// An invalid login reaches its own controller after the refresh bucket is
	// exhausted; no credentials or database access are needed for this contract.
	login := request("/api/user/login", "https://dashboard.example")
	assert.Equal(t, http.StatusOK, login.Code)
	assert.Contains(t, login.Body.String(), `"success":false`)
}
