package router

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type browserBoundOAuthProvider struct {
	userID           int
	calls            atomic.Int32
	temporaryFailure atomic.Bool
}

func (*browserBoundOAuthProvider) GetName() string { return "Browser Binding" }
func (*browserBoundOAuthProvider) IsEnabled() bool { return true }
func (p *browserBoundOAuthProvider) ExchangeToken(_ context.Context, code string, _ *gin.Context) (*oauth.OAuthToken, error) {
	p.calls.Add(1)
	if p.temporaryFailure.Load() || code == "temporary-failure" {
		return nil, fmt.Errorf("temporary provider failure")
	}
	return &oauth.OAuthToken{}, nil
}
func (*browserBoundOAuthProvider) GetUserInfo(context.Context, *oauth.OAuthToken) (*oauth.OAuthUser, error) {
	return &oauth.OAuthUser{ProviderUserID: "browser-user"}, nil
}
func (*browserBoundOAuthProvider) IsUserIDTaken(string) bool { return true }
func (p *browserBoundOAuthProvider) FillUserByProviderID(user *model.User, _ string) error {
	return model.DB.First(user, p.userID).Error
}
func (*browserBoundOAuthProvider) SetProviderUserID(*model.User, string) {}
func (*browserBoundOAuthProvider) GetProviderPrefix() string             { return "browser_" }
func (*browserBoundOAuthProvider) ProviderUserIDColumn() string          { return "github_id" }

func oauthBrowserRouter(t *testing.T) (*gin.Engine, *browserBoundOAuthProvider) {
	t.Helper()
	require.NoError(t, i18n.Init())
	gin.SetMode(gin.TestMode)
	oldDB, oldLog, oldRedis, oldSecret, oldSecure := model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret, common.SessionCookieSecure
	oldType := common.MainDatabaseType()
	oldGlobal, oldCritical := common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "oauth.db")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.TwoFA{}, &model.TwoFABackupCode{}, &model.PasskeyCredential{}, &model.AuthFlow{}, &model.UserOAuthBinding{}, &model.Option{}, &model.UserAccessToken{}, &model.AuditLog{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SessionSecret = "browser-binding-test-secret"
	common.SessionCookieSecure = false
	common.GlobalApiRateLimitEnable = false
	common.CriticalRateLimitEnable = false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	user := model.User{Username: "browser-login", AffCode: "browser-login", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	provider := &browserBoundOAuthProvider{userID: user.Id}
	oauth.Register("browser-binding", provider)
	t.Cleanup(func() {
		oauth.Unregister("browser-binding")
		model.DB, model.LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.SessionSecret = oldSecret
		common.SessionCookieSecure = oldSecure
		common.SetMainDatabaseType(oldType)
		common.GlobalApiRateLimitEnable, common.CriticalRateLimitEnable = oldGlobal, oldCritical
		sqlDB.Close()
	})
	engine := gin.New()
	SetApiRouter(engine)
	engine.GET("/oauth-probe", func(c *gin.Context) {
		c.Header("Content-Type", "text/html")
		c.String(200, "<!doctype html><title>OAuth cookie probe</title>")
	})
	return engine, provider
}

func startBrowserOAuth(t *testing.T, engine *gin.Engine) (string, *http.Cookie) {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/oauth/state", strings.NewReader(`{"provider":"browser-binding","intent":"login"}`))
	r.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, r)
	require.Equal(t, 200, w.Code)
	var body struct {
		Success bool
		Data    struct {
			FlowToken string `json:"flow_token"`
		}
	}
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
	require.True(t, body.Success, w.Body.String())
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if strings.HasPrefix(c.Name, "ren2hub_oauth_") {
			cookie = c
		}
	}
	return body.Data.FlowToken, cookie
}
func callbackBrowserOAuth(engine *gin.Engine, state, extra string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "/api/oauth/browser-binding?state="+url.QueryEscape(state)+"&"+extra, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, r)
	return w
}

func TestOAuthLoginRoutesRequireOriginatingBrowser(t *testing.T) {
	for _, kind := range []string{"missing", "wrong", "foreign", "legacy", "expired", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			engine, provider := oauthBrowserRouter(t)
			state, issuedCookie := startBrowserOAuth(t, engine)
			var cookie *http.Cookie
			if kind == "wrong" {
				sum := sha256.Sum256([]byte(state))
				cookie = &http.Cookie{Name: fmt.Sprintf("ren2hub_oauth_%x", sum[:12]), Value: "wrong"}
			}
			if kind == "foreign" {
				_, cookie = startBrowserOAuth(t, engine)
			}

			if kind == "legacy" {
				var err error
				state, _, err = model.CreateAuthFlow(model.AuthFlowCreate{Purpose: model.AuthFlowPurposeOAuth, Provider: "browser-binding", Intent: model.AuthFlowIntentLogin, Payload: `{}`, ExpiresAt: time.Now().Add(time.Minute)})
				require.NoError(t, err)
			}
			if kind == "expired" {
				cookie = issuedCookie
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeOAuth).Update("expires_at", time.Now().Add(-time.Minute)).Error)
			}
			var w *httptest.ResponseRecorder
			if kind == "duplicate" {
				r := httptest.NewRequest("GET", "/api/oauth/browser-binding?state="+state+"&code=valid", nil)
				require.NotNil(t, issuedCookie)
				r.AddCookie(issuedCookie)
				r.AddCookie(issuedCookie)
				w = httptest.NewRecorder()
				engine.ServeHTTP(w, r)
			} else {
				w = callbackBrowserOAuth(engine, state, "code=valid", cookie)
			}
			require.Equal(t, 403, w.Code)
			var body map[string]any
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, false, body["success"])
			if kind == "expired" {
				require.NotContains(t, body, "code")
			} else {
				require.Equal(t, "OAUTH_FLOW_BROWSER_MISMATCH", body["code"])
			}
			require.Zero(t, provider.calls.Load())
			require.Empty(t, w.Result().Cookies())
			_, err := model.GetAuthFlow(state, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
			if kind == "expired" {
				require.ErrorIs(t, err, model.ErrAuthFlowExpired)
			} else {
				require.NoError(t, err)
			}
			var sessions int64
			require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessions).Error)
			require.Zero(t, sessions)
		})
	}
}

func TestOAuthLoginBrowserCookieLifecycle(t *testing.T) {
	engine, provider := oauthBrowserRouter(t)
	common.SessionCookieSecure = true
	state, cookie := startBrowserOAuth(t, engine)
	require.NotNil(t, cookie)
	sum := sha256.Sum256([]byte(state))
	require.Equal(t, fmt.Sprintf("ren2hub_oauth_%x", sum[:12]), cookie.Name)
	require.Equal(t, "/api/oauth/", cookie.Path)
	require.Empty(t, cookie.Domain)
	require.True(t, cookie.HttpOnly)
	require.True(t, cookie.Secure)
	require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	require.Equal(t, 600, cookie.MaxAge)
	require.Len(t, cookie.Value, 43)
	flow, err := model.GetAuthFlow(state, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
	require.NoError(t, err)
	require.NotContains(t, flow.Payload, cookie.Value)
	second, secondCookie := startBrowserOAuth(t, engine)
	require.NotNil(t, secondCookie)
	require.NotEqual(t, cookie.Name, secondCookie.Name)
	require.NotEqual(t, cookie.Value, secondCookie.Value)
	provider.temporaryFailure.Store(true)
	failed := callbackBrowserOAuth(engine, state, "code=valid", cookie)
	require.Empty(t, failed.Result().Cookies())
	_, err = model.GetAuthFlow(state, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
	require.NoError(t, err)
	provider.temporaryFailure.Store(false)
	ok := callbackBrowserOAuth(engine, state, "code=valid", cookie)
	require.Equal(t, 200, ok.Code)
	require.Contains(t, ok.Body.String(), `"success":true`)
	found := false
	refresh := false
	for _, c := range ok.Result().Cookies() {
		if c.Name == cookie.Name {
			found = true
			require.Equal(t, -1, c.MaxAge)
			require.Equal(t, cookie.Path, c.Path)
		}
		if c.Name == service.RefreshCookieName {
			refresh = true
		}
	}
	require.True(t, found)
	require.True(t, refresh)
	before := provider.calls.Load()
	replay := callbackBrowserOAuth(engine, state, "code=valid", cookie)
	require.Equal(t, 403, replay.Code)
	var replayBody map[string]any
	require.NoError(t, common.Unmarshal(replay.Body.Bytes(), &replayBody))
	require.NotContains(t, replayBody, "code")
	require.Equal(t, before, provider.calls.Load())
	cancelled := callbackBrowserOAuth(engine, second, "error=access_denied", secondCookie)
	require.Equal(t, 200, cancelled.Code)
	require.Equal(t, before, provider.calls.Load())
	_, err = model.GetAuthFlow(second, model.AuthFlowMatch{Purpose: model.AuthFlowPurposeOAuth})
	require.ErrorIs(t, err, model.ErrAuthFlowConsumed)
	require.Len(t, cancelled.Result().Cookies(), 1)
	require.Equal(t, secondCookie.Name, cancelled.Result().Cookies()[0].Name)
	require.Equal(t, -1, cancelled.Result().Cookies()[0].MaxAge)
}

func TestOAuthBrowserMismatchCodePreservesOtherForbiddenResponses(t *testing.T) {
	for _, kind := range []string{"invalid_state", model.AuthFlowIntentBind, model.AuthFlowIntentVerify} {
		t.Run(kind, func(t *testing.T) {
			engine, provider := oauthBrowserRouter(t)
			state := "not-a-valid-flow"
			if kind != "invalid_state" {
				var err error
				state, _, err = model.CreateAuthFlow(model.AuthFlowCreate{
					Purpose: model.AuthFlowPurposeOAuth, Provider: "browser-binding", Intent: kind,
					UserId: provider.userID, SessionId: "another-dashboard-session", Payload: `{}`,
					ExpiresAt: time.Now().Add(time.Minute),
				})
				require.NoError(t, err)
			}
			response := callbackBrowserOAuth(engine, state, "code=valid", nil)
			require.Equal(t, http.StatusForbidden, response.Code)
			var body map[string]any
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, false, body["success"])
			require.NotContains(t, body, "code")
			require.NotEmpty(t, body["message"])
			require.Empty(t, response.Result().Cookies())
			require.Zero(t, provider.calls.Load())
		})
	}
}
