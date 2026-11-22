package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestWeChatLoginRouteRejectsCrossOriginBrowserRequests(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		allowed bool
	}{
		{"non-browser", nil, true}, {"same-origin", []string{"same-origin"}, true},
		{"same-site", []string{"same-site"}, false}, {"cross-site", []string{"cross-site"}, false},
		{"navigation", []string{"none"}, false}, {"empty", []string{""}, false},
		{"duplicate", []string{"same-origin", "cross-site"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, provider := oauthBrowserRouter(t)
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", provider.userID).Update("wechat_id", "wechat-test-user").Error)
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "/api/wechat/user", r.URL.Path)
				require.Equal(t, "valid-code", r.URL.Query().Get("code"))
				fmt.Fprint(w, `{"success":true,"data":"wechat-test-user"}`)
			}))
			t.Cleanup(upstream.Close)
			oldEnabled, oldURL, oldToken := common.WeChatAuthEnabled, common.WeChatServerAddress, common.WeChatServerToken
			common.WeChatAuthEnabled = true
			common.WeChatServerAddress = upstream.URL
			common.WeChatServerToken = "local-test-only"
			t.Cleanup(func() {
				common.WeChatAuthEnabled = oldEnabled
				common.WeChatServerAddress = oldURL
				common.WeChatServerToken = oldToken
			})
			for attempt := 0; attempt < 2; attempt++ {
				req := httptest.NewRequest(http.MethodGet, "/api/oauth/wechat?code=valid-code", nil)
				for _, value := range tc.headers {
					req.Header.Add("Sec-Fetch-Site", value)
				}
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, req)
				if tc.allowed {
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.Contains(t, w.Body.String(), `"access_token"`)
					require.NotEmpty(t, w.Result().Cookies())
				} else {
					require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
					require.NotContains(t, w.Body.String(), `"access_token"`)
					require.Empty(t, w.Result().Cookies())
					require.Zero(t, calls.Load(), "rejected requests cannot exchange authorization codes")
				}
				var count int64
				require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&count).Error)
				if tc.allowed {
					require.EqualValues(t, attempt+1, count)
				} else {
					require.Zero(t, count)
				}
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Count(&count).Error)
				require.Zero(t, count)
				require.NoError(t, model.DB.Model(&model.User{}).Count(&count).Error)
				require.EqualValues(t, 1, count)
			}
			if !tc.allowed {
				req := httptest.NewRequest(http.MethodGet, "/api/oauth/wechat?code=valid-code", nil)
				req.Header.Set("Sec-Fetch-Site", "same-origin")
				w := httptest.NewRecorder()
				engine.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), `"access_token"`)
				require.EqualValues(t, 1, calls.Load(), "rejection does not consume the code or block a legitimate retry")
			}
		})
	}
}
