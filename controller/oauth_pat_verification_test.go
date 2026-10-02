package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func oauthVerificationHTTPRouter() *gin.Engine {
	middleware.DeclarePATRoute(http.MethodPost, "/api/oauth/state", "verification:write")
	middleware.DeclarePATRoute(http.MethodGet, "/api/oauth/:provider", "verification:write")
	engine := gin.New()
	engine.POST("/api/oauth/state", middleware.TryUserAuth(), GenerateOAuthCode)
	engine.GET("/api/oauth/:provider", middleware.TryUserAuth(), HandleOAuth)
	return engine
}

func oauthVerificationHTTPRequest(engine *gin.Engine, method, path, body, bearer string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func startManagementOAuthVerification(t *testing.T, engine *gin.Engine, provider, bearer string, telegram *telegramOAuthFixture) string {
	t.Helper()
	response := oauthVerificationHTTPRequest(engine, http.MethodPost, "/api/oauth/state", fmt.Sprintf(`{"provider":%q,"intent":"verify","scope":"admin.user.change","context":{"ids":[2],"action":"disable"}}`, provider), bearer)
	var body securityEnrollmentResponse
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
	require.True(t, body.Success, body.Message)
	var data struct {
		State string `json:"flow_token"`
		URL   string `json:"authorization_url"`
	}
	require.NoError(t, common.Unmarshal(body.Data, &data))
	require.NotEmpty(t, data.State)
	code := "verification-code"
	if telegram != nil {
		authorizationURL, err := url.Parse(data.URL)
		require.NoError(t, err)
		code += "-" + data.State
		telegram.mutex.Lock()
		telegram.grants[code] = telegramTestGrant{challenge: authorizationURL.Query().Get("code_challenge"), claims: telegramIdentityClaims(42), key: telegram.key}
		telegram.mutex.Unlock()
	}
	return "/api/oauth/" + provider + "?" + url.Values{"state": {data.State}, "code": {code}}.Encode()
}

func TestPATOAuthVerificationHTTPPreservesIdentityAndProofBinding(t *testing.T) {
	for _, provider := range []string{"pat-verification-oauth", "telegram"} {
		for _, credential := range []string{"browser", "PAT"} {
			t.Run(provider+"/"+credential, func(t *testing.T) {
				var user *model.User
				var identity service.AuthIdentity
				var telegram *telegramOAuthFixture
				if provider == "telegram" {
					telegram = setupTelegramOAuthTest(t)
					user, identity = telegram.user, telegram.identity
				} else {
					user, identity = setupSecurityEnrollmentTest(t)
					oauth.Register(provider, &enrollmentOAuthProvider{externalID: "linked-account"})
					t.Cleanup(func() { oauth.Unregister(provider) })
				}
				require.NoError(t, model.DB.Model(user).Updates(map[string]any{"role": common.RoleRootUser, "password": "", "github_id": "linked-account", "telegram_id": "42"}).Error)
				bearer, _, err := service.IssueAccessToken(identity)
				require.NoError(t, err)
				if credential == "PAT" {
					pat, raw, err := model.CreatePersonalAccessToken(identity, "OAuth management", []string{"users:write", "verification:write"}, 0)
					require.NoError(t, err)
					bearer = raw
					identity.PersonalAccessTokenID, identity.SessionID, identity.SessionVersion = pat.ID, fmt.Sprintf("pat:%d", pat.ID), 1
				}
				engine := oauthVerificationHTTPRouter()
				path := startManagementOAuthVerification(t, engine, provider, bearer, telegram)
				response := oauthVerificationHTTPRequest(engine, http.MethodGet, path, "", bearer)
				require.Equal(t, http.StatusOK, response.Code)
				var body securityEnrollmentResponse
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				require.True(t, body.Success, body.Message)
				var proof service.SecurityProof
				require.NoError(t, common.Unmarshal(body.Data, &proof))
				require.NotEmpty(t, proof.ProofToken)
				assert.Equal(t, service.VerificationMethodOAuth, proof.Method)
				assert.NotContains(t, response.Body.String(), "access_token")
				assert.Empty(t, response.Header().Values("Set-Cookie"))
				_, err = service.ConsumeOperationProof(proof.ProofToken, identity, service.VerificationOperation{Scope: service.VerificationScopeAdminUserChange, Context: []byte(`{"ids":[3],"action":"disable"}`)})
				require.Error(t, err)
				operation := service.VerificationOperation{Scope: service.VerificationScopeAdminUserChange, Context: []byte(`{"ids":[2],"action":"disable"}`)}
				_, err = service.ConsumeOperationProof(proof.ProofToken, identity, operation)
				require.NoError(t, err)
				_, err = service.ConsumeOperationProof(proof.ProofToken, identity, operation)
				assert.ErrorIs(t, err, service.ErrProofConsumed)
				replay := oauthVerificationHTTPRequest(engine, http.MethodGet, path, "", bearer)
				assert.Equal(t, http.StatusForbidden, replay.Code)
				var users, sessions, bindings int64
				require.NoError(t, model.DB.Model(&model.User{}).Count(&users).Error)
				require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessions).Error)
				require.NoError(t, model.DB.Model(&model.UserOAuthBinding{}).Count(&bindings).Error)
				assert.EqualValues(t, 1, users)
				assert.EqualValues(t, 1, sessions)
				assert.Zero(t, bindings)
			})
		}
	}
}

func TestPATOAuthVerificationHTTPRejectsChangedIdentity(t *testing.T) {
	for _, provider := range []string{"pat-verification-oauth", "telegram"} {
		for _, scenario := range []string{"missing", "different PAT", "revoked", "expired", "flow expired", "binding changed", "auth version changed"} {
			t.Run(provider+"/"+scenario, func(t *testing.T) {
				var user *model.User
				var identity service.AuthIdentity
				var telegram *telegramOAuthFixture
				if provider == "telegram" {
					telegram = setupTelegramOAuthTest(t)
					user, identity = telegram.user, telegram.identity
				} else {
					user, identity = setupSecurityEnrollmentTest(t)
					oauth.Register(provider, &enrollmentOAuthProvider{externalID: "linked-account"})
					t.Cleanup(func() { oauth.Unregister(provider) })
				}
				require.NoError(t, model.DB.Model(user).Updates(map[string]any{"role": common.RoleRootUser, "password": "", "github_id": "linked-account", "telegram_id": "42"}).Error)
				pat, bearer, err := model.CreatePersonalAccessToken(identity, "OAuth management", []string{"users:write", "verification:write"}, 0)
				require.NoError(t, err)
				engine := oauthVerificationHTTPRouter()
				path := startManagementOAuthVerification(t, engine, provider, bearer, telegram)
				switch scenario {
				case "missing":
					bearer = ""
				case "different PAT":
					_, bearer, err = model.CreatePersonalAccessToken(identity, "Other identity", []string{"users:write", "verification:write"}, 0)
					require.NoError(t, err)
				case "revoked":
					require.NoError(t, model.DB.Model(pat).Update("revoked_at", time.Now().Unix()).Error)
				case "expired":
					require.NoError(t, model.DB.Model(pat).Update("expires_at", time.Now().Add(-time.Second).Unix()).Error)
				case "flow expired":
					require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeOAuth).Update("expires_at", time.Now().Add(-time.Second)).Error)
				case "binding changed":
					require.NoError(t, model.DB.Model(user).Updates(map[string]any{"github_id": "other-account", "telegram_id": "43"}).Error)
				case "auth version changed":
					require.NoError(t, model.DB.Model(user).Update("auth_version", 2).Error)
				}
				response := oauthVerificationHTTPRequest(engine, http.MethodGet, path, "", bearer)
				assert.Contains(t, response.Body.String(), `"success":false`)
				assert.NotContains(t, response.Body.String(), "proof_token")
				assert.Empty(t, response.Header().Values("Set-Cookie"))
				var users, sessions, proofs, bindings int64
				require.NoError(t, model.DB.Model(&model.User{}).Count(&users).Error)
				require.NoError(t, model.DB.Model(&model.UserSession{}).Count(&sessions).Error)
				require.NoError(t, model.DB.Model(&model.AuthFlow{}).Where("purpose = ?", model.AuthFlowPurposeSecurityProof).Count(&proofs).Error)
				require.NoError(t, model.DB.Model(&model.UserOAuthBinding{}).Count(&bindings).Error)
				assert.EqualValues(t, 1, users)
				assert.EqualValues(t, 1, sessions)
				assert.Zero(t, proofs)
				assert.Zero(t, bindings)
			})
		}
	}
}
