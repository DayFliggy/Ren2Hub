package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupDashboardAuthMiddlewareTest(t *testing.T) {
	t.Helper()
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousRedis := common.RedisEnabled
	previousSecret := common.SessionSecret
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.PersonalAccessToken{}, &model.UserSession{}))
	model.DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.SessionSecret = "middleware-auth-test-secret"
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetMainDatabaseType(previousType)
		common.RedisEnabled = previousRedis
		common.SessionSecret = previousSecret
	})
}

func issueExpiredDashboardAccessToken(t *testing.T, identity service.AuthIdentity) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":       "new-api",
		"aud":       []string{"new-api-dashboard"},
		"sub":       fmt.Sprintf("%d", identity.UserID),
		"token_use": "access",
		"sid":       identity.SessionID,
		"uv":        identity.UserAuthVersion,
		"sv":        identity.SessionVersion,
		"exp":       time.Now().Add(-time.Minute).Unix(),
		"nbf":       time.Now().Add(-2 * time.Minute).Unix(),
		"iat":       time.Now().Add(-2 * time.Minute).Unix(),
	}
	mac := hmac.New(sha256.New, []byte(common.SessionSecret))
	_, err := mac.Write([]byte("new-api/auth/access/v1"))
	require.NoError(t, err)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(mac.Sum(nil))
	require.NoError(t, err)
	return token
}

func tamperDashboardToken(token string) string {
	tamperAt := len(token) - 2
	replacement := "x"
	if token[tamperAt] == 'x' {
		replacement = "y"
	}
	return token[:tamperAt] + replacement + token[tamperAt+1:]
}

func createMiddlewarePATUser(t *testing.T, username, token string) *model.User {
	t.Helper()
	user := &model.User{
		Username: username, Password: "password-placeholder", Role: common.RoleCommonUser,
		Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1,
		AffCode: "middleware-aff-" + username,
	}
	require.NoError(t, model.DB.Create(user).Error)
	require.NoError(t, model.DB.Create(&model.PersonalAccessToken{UserID: user.Id, Name: "test", Digest: model.PersonalAccessTokenDigest(token), Scopes: []string{"profile:read"}, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
	return user
}

// Read-only API key routes (/api/usage/token, /api/log/token) are called by
// scripts, not the web console, so their errors are translated by the backend
// in the language the request asks for, English when it asks for none.
func TestTokenAuthReadOnlyTranslatesErrors(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		acceptLanguage string
		want           string
	}{
		{"zh-CN", "未提供令牌"},
		{"en-US", "Token not provided"},
		{"", "Token not provided"},
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/log/token", nil)
		if tc.acceptLanguage != "" {
			c.Request.Header.Set("Accept-Language", tc.acceptLanguage)
		}
		TokenAuthReadOnly()(c)
		assert.True(t, c.IsAborted(), tc.acceptLanguage)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code, tc.acceptLanguage)
		assert.JSONEq(t, `{"success":false,"message":"`+tc.want+`"}`, recorder.Body.String(), tc.acceptLanguage)
	}
}

func TestUserAuthAllowsOpaqueDottedPAT(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	user := createMiddlewarePATUser(t, "dotted-pat-user", "pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	router := gin.New()
	DeclarePATRoute("GET", "/protected", "profile:read")
	router.GET("/protected", UserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.GetInt("id")})
	})
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer pat_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	var body struct {
		ID int `json:"id"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	assert.Equal(t, user.Id, body.ID)
}

func TestUserAuthNeverFallsBackForRecognizedInvalidInternalJWT(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	identity := service.AuthIdentity{UserID: 42, SessionID: "session-42", UserAuthVersion: 1, SessionVersion: 1}
	token, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	tampered := tamperDashboardToken(token)
	createMiddlewarePATUser(t, "jwt-fallback-user", tampered)
	router := gin.New()
	router.GET("/protected", UserAuth(), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Set("Authorization", "Bearer "+tampered)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "AUTH_UNAUTHORIZED")
}

func TestTryUserAuthCredentialClassification(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	gin.SetMode(gin.TestMode)

	patUser := createMiddlewarePATUser(t, "optional-pat-user", "pat_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	internalUser := createMiddlewarePATUser(t, "optional-session-user", "unrelated-pat")
	now := time.Now().Unix()
	session := &model.UserSession{
		SID:             "optional-auth-session",
		UserID:          internalUser.Id,
		Version:         1,
		UserAuthVersion: internalUser.AuthVersion,
		Status:          model.UserSessionStatusActive,
		RefreshHash:     "refresh-hash",
		LoginMethod:     "password",
		LastActiveAt:    now,
		ExpiresAt:       now + 3600,
	}
	require.NoError(t, model.CreateUserSession(session))
	identity := service.AuthIdentity{
		UserID:          internalUser.Id,
		SessionID:       session.SID,
		UserAuthVersion: session.UserAuthVersion,
		SessionVersion:  session.Version,
	}
	accessToken, _, err := service.IssueAccessToken(identity)
	require.NoError(t, err)
	require.NoError(t, model.DB.AutoMigrate(&model.AuthFlow{}))
	securityProof, _, err := service.IssueSecurityProof(identity, "2fa", service.VerificationBinding{Scope: "channel.key.read", ContextHash: "test-context"})
	require.NoError(t, err)
	externalToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"iss": "external-issuer",
		"aud": "external-audience",
		"exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString([]byte("external-secret"))
	require.NoError(t, err)

	router := gin.New()
	DeclarePATRoute("GET", "/optional", "profile:read")
	router.GET("/optional", TryUserAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"id":               c.GetInt("id"),
			"use_access_token": c.GetBool("use_access_token"),
		})
	})

	tests := []struct {
		name          string
		token         string
		wantStatus    int
		wantUserID    int
		wantPAT       bool
		wantErrorCode string
	}{
		{name: "no authorization header", wantStatus: http.StatusOK},
		{name: "opaque unmatched credential", token: "opaque-relay-key", wantStatus: http.StatusOK},
		{name: "dotted unmatched credential", token: "ordinary.key.with-dots", wantStatus: http.StatusOK},
		{name: "third party jwt", token: externalToken, wantStatus: http.StatusOK},
		{name: "valid pat", token: "pat_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", wantStatus: http.StatusOK, wantUserID: patUser.Id, wantPAT: true},
		{name: "valid internal access jwt", token: accessToken, wantStatus: http.StatusOK, wantUserID: internalUser.Id},
		{name: "expired internal access jwt", token: issueExpiredDashboardAccessToken(t, identity), wantStatus: http.StatusUnauthorized, wantErrorCode: "AUTH_TOKEN_EXPIRED"},
		{name: "tampered internal access jwt", token: tamperDashboardToken(accessToken), wantStatus: http.StatusUnauthorized, wantErrorCode: "AUTH_UNAUTHORIZED"},
		{name: "security proof used as access", token: securityProof, wantStatus: http.StatusUnauthorized, wantErrorCode: "AUTH_UNAUTHORIZED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/optional", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, test.wantStatus, response.Code)
			if test.wantErrorCode != "" {
				assert.Contains(t, response.Body.String(), test.wantErrorCode)
				return
			}
			var body struct {
				ID             int  `json:"id"`
				UseAccessToken bool `json:"use_access_token"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, test.wantUserID, body.ID)
			assert.Equal(t, test.wantPAT, body.UseAccessToken)
		})
	}

	requiredRouter := gin.New()
	requiredRouter.GET("/required", UserAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	requiredRequest := httptest.NewRequest(http.MethodGet, "/required", nil)
	requiredRequest.Header.Set("Authorization", "Bearer ordinary-unmatched-key")
	requiredResponse := httptest.NewRecorder()
	requiredRouter.ServeHTTP(requiredResponse, requiredRequest)
	assert.Equal(t, http.StatusUnauthorized, requiredResponse.Code, "required dashboard authentication must not adopt optional-auth fallback semantics")

	var patUserQueries int
	forcedCacheError := errors.New("forced PAT user cache lookup failure")
	const callbackName = "test:optional-auth-pat-user-cache-failure"
	require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "users" {
			return
		}
		patUserQueries++
		if patUserQueries == 1 {
			tx.AddError(forcedCacheError)
		}
	}))
	cacheFailureRequest := httptest.NewRequest(http.MethodGet, "/optional", nil)
	cacheFailureRequest.Header.Set("Authorization", "Bearer pat_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	cacheFailureResponse := httptest.NewRecorder()
	router.ServeHTTP(cacheFailureResponse, cacheFailureRequest)
	model.DB.Callback().Query().Remove(callbackName)
	assert.Equal(t, http.StatusInternalServerError, cacheFailureResponse.Code)
	assert.Contains(t, cacheFailureResponse.Body.String(), "AUTH_INTERNAL_ERROR")

	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	databaseFailureRequest := httptest.NewRequest(http.MethodGet, "/optional", nil)
	databaseFailureRequest.Header.Set("Authorization", "Bearer pat_cccccccccccccccccccccccccccccccccccccccccccccccc")
	databaseFailureResponse := httptest.NewRecorder()
	router.ServeHTTP(databaseFailureResponse, databaseFailureRequest)
	assert.Equal(t, http.StatusInternalServerError, databaseFailureResponse.Code)
	assert.Contains(t, databaseFailureResponse.Body.String(), "AUTH_INTERNAL_ERROR")
}
