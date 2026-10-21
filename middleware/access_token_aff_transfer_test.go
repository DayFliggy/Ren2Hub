package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 旧返利转入路由与新路由同样只接受浏览器会话：带 wallet:write 的 scoped PAT 和 legacy PAT 都被拒绝。
func TestAffTransferRequiresBrowserSession(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	gin.SetMode(gin.TestMode)
	legacy := "middleware-aff-transfer-legacy"
	user := createMiddlewarePATUser(t, "aff-transfer-user", legacy)
	scoped, _ := createMiddlewareScopedToken(t, user.Id, 0, "wallet:write")
	session, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "test")
	require.NoError(t, err)
	router := gin.New()
	router.POST("/api/user/aff_transfer", UserAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, test := range []struct {
		name, token string
		status      int
	}{
		{"scoped token with wallet:write", scoped, http.StatusForbidden},
		{"legacy token", legacy, http.StatusForbidden},
		{"browser session", session.AccessToken, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/user/aff_transfer", nil)
			request.Header.Set("Authorization", "Bearer "+test.token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, test.status, response.Code, response.Body.String())
			if test.status == http.StatusForbidden {
				assert.Contains(t, response.Body.String(), `"code":"AUTH_SESSION_REQUIRED"`)
			}
		})
	}
}
