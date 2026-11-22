package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 既有提供商测试也必须携带由真实发起接口写出的浏览器 Cookie。
var oauthBrowserTestCookies sync.Map

func rememberOAuthTestCookie(t *testing.T, state string, response *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if strings.HasPrefix(cookie.Name, "ren2hub_oauth_") {
			oauthBrowserTestCookies.Store(state, cookie)
			t.Cleanup(func() { oauthBrowserTestCookies.Delete(state) })
			return
		}
	}
}
func startOAuthLoginForTest(t *testing.T, provider string) string {
	t.Helper()
	body, err := common.Marshal(map[string]string{"provider": provider, "intent": "login"})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/oauth/state", strings.NewReader(string(body)))
	GenerateOAuthCode(c)
	var result struct {
		Success bool
		Data    struct {
			FlowToken string `json:"flow_token"`
		}
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Success, w.Body.String())
	rememberOAuthTestCookie(t, result.Data.FlowToken, w)
	_, ok := oauthBrowserTestCookies.Load(result.Data.FlowToken)
	require.True(t, ok, "login start must set its binding cookie")
	return result.Data.FlowToken
}
func oauthTestCallbackRequest(request *http.Request) *http.Request {
	if cookie, ok := oauthBrowserTestCookies.Load(request.URL.Query().Get("state")); ok {
		request.AddCookie(cookie.(*http.Cookie))
	}
	return request
}
func oauthSessionCookieHeaders(response *httptest.ResponseRecorder) []string {
	var result []string
	for _, cookie := range response.Result().Cookies() {
		if strings.HasPrefix(cookie.Name, "ren2hub_oauth_") && cookie.MaxAge < 0 {
			continue
		}
		result = append(result, cookie.String())
	}
	return result
}
