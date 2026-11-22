package controller

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func oauthBrowserCookieName(state string) string {
	digest := sha256.Sum256([]byte(state))
	return "ren2hub_oauth_" + hex.EncodeToString(digest[:12])
}

func oauthBrowserNonceHash(nonce string) string {
	return common.GenerateHMACWithKey([]byte("oauth-browser-v1:"+common.SessionSecret), nonce)
}

func newOAuthBrowserNonce() (string, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(nonce), nil
}

func writeOAuthBrowserCookie(c *gin.Context, state, nonce string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{Name: oauthBrowserCookieName(state), Value: nonce, Path: "/api/oauth/", MaxAge: int(oauthAuthFlowTTL / time.Second), Expires: expires, HttpOnly: true, Secure: common.SessionCookieSecure, SameSite: http.SameSiteLaxMode})
}

func clearOAuthBrowserCookie(c *gin.Context, state, intent string) {
	if intent != model.AuthFlowIntentLogin {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: oauthBrowserCookieName(state), Path: "/api/oauth/", MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: true, Secure: common.SessionCookieSecure, SameSite: http.SameSiteLaxMode})
}

// 登录 flow 必须绑定发起它的浏览器；state 本身不构成浏览器身份证明。
func validateOAuthBrowserBinding(c *gin.Context, state string, flow *model.AuthFlow) error {
	if flow.Intent != model.AuthFlowIntentLogin {
		return nil
	}
	var payload oauthFlowPayload
	if err := common.UnmarshalJsonStr(flow.Payload, &payload); err != nil || payload.BrowserNonceHash == "" {
		return model.ErrAuthFlowInvalid
	}
	cookies := c.Request.CookiesNamed(oauthBrowserCookieName(state))
	if len(cookies) != 1 {
		return model.ErrAuthFlowInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(cookies[0].Value)
	if err != nil || len(raw) != 32 {
		return model.ErrAuthFlowInvalid
	}
	if !hmac.Equal([]byte(payload.BrowserNonceHash), []byte(oauthBrowserNonceHash(cookies[0].Value))) {
		return model.ErrAuthFlowInvalid
	}
	return nil
}
