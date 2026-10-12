package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func withHeaderNavModules(t *testing.T, raw string) {
	t.Helper()

	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = map[string]string{}
	}
	previous, hadPrevious := common.OptionMap["HeaderNavModules"]
	common.OptionMap["HeaderNavModules"] = raw
	common.OptionMapRWMutex.Unlock()

	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if hadPrevious {
			common.OptionMap["HeaderNavModules"] = previous
			return
		}
		delete(common.OptionMap, "HeaderNavModules")
	})
}

func performHeaderNavRequest(t *testing.T, handler gin.HandlerFunc, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/test", handler, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	var accessToken string
	if authenticated {
		previousDB, previousRedis := model.DB, common.RedisEnabled
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&model.User{}, &model.PersonalAccessToken{}))
		model.DB = db
		common.RedisEnabled = false
		t.Cleanup(func() {
			model.DB = previousDB
			common.RedisEnabled = previousRedis
		})
		accessToken = "pat_hhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhhh"
		user := model.User{
			Username:    "tester",
			Password:    "unused-password-hash",
			Role:        common.RoleCommonUser,
			Status:      common.UserStatusEnabled,
			Group:       "default",
			AuthVersion: 1,
		}
		require.NoError(t, db.Create(&user).Error)
		require.NoError(t, db.Create(&model.PersonalAccessToken{UserID: user.Id, Name: "nav", Digest: model.PersonalAccessTokenDigest(accessToken), Scopes: []string{"profile:read"}, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
		DeclarePATRoute("GET", "/api/test", "profile:read")
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	if authenticated {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestHeaderNavModuleAuthAllowsDefaultPublicAccess(t *testing.T) {
	withHeaderNavModules(t, "")

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("pricing"), false)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHeaderNavModuleAuthRejectsDisabledPricing(t *testing.T) {
	raw := `{"pricing":{"enabled":false,"requireAuth":false}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("pricing"), false)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestDisabledPricingRequiresCurrentRootAuthentication(t *testing.T) {
	for _, tc := range []struct {
		name, module, options, credential string
		role, wantStatus                  int
	}{
		{name: "root session", role: common.RoleRootUser, wantStatus: http.StatusOK},
		{name: "root login required", options: `{"pricing":{"enabled":false,"requireAuth":true}}`, role: common.RoleRootUser, wantStatus: http.StatusOK},
		{name: "root legacy setting", options: `{"pricing":false}`, role: common.RoleRootUser, wantStatus: http.StatusOK},
		{name: "ordinary user", role: common.RoleCommonUser, wantStatus: http.StatusForbidden},
		{name: "administrator", role: common.RoleAdminUser, wantStatus: http.StatusForbidden},
		{name: "expired session", role: common.RoleRootUser, credential: "expired", wantStatus: http.StatusUnauthorized},
		{name: "tampered session", role: common.RoleRootUser, credential: "tampered", wantStatus: http.StatusUnauthorized},
		{name: "revoked session", role: common.RoleRootUser, credential: "revoked", wantStatus: http.StatusUnauthorized},
		{name: "disabled account", role: common.RoleRootUser, credential: "disabled", wantStatus: http.StatusUnauthorized},
		{name: "demoted root", role: common.RoleRootUser, credential: "demoted", wantStatus: http.StatusForbidden},
		{name: "root PAT cannot bypass undeclared route", role: common.RoleRootUser, credential: "pat", wantStatus: http.StatusForbidden},
		{name: "root cannot bypass other module", module: "rankings", options: `{"rankings":false}`, role: common.RoleRootUser, wantStatus: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setupDashboardAuthMiddlewareTest(t)
			options := tc.options
			if options == "" {
				options = `{"pricing":{"enabled":false,"requireAuth":false}}`
			}
			withHeaderNavModules(t, options)
			user := model.User{Username: "pricing-reader", Role: tc.role, Status: common.UserStatusEnabled, Group: "default", AuthVersion: 1}
			require.NoError(t, model.DB.Create(&user).Error)
			bundle, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "header-nav-test")
			require.NoError(t, err)
			token := bundle.AccessToken
			switch tc.credential {
			case "expired":
				identity, internal, err := service.ParseDashboardAccessToken(token)
				require.NoError(t, err)
				require.True(t, internal)
				token = issueExpiredDashboardAccessToken(t, identity)
			case "tampered":
				token = tamperDashboardToken(token)
			case "revoked":
				revoked, err := model.RevokeUserSession(user.Id, bundle.Session.SID, "test")
				require.NoError(t, err)
				require.True(t, revoked)
			case "disabled":
				require.NoError(t, model.DB.Model(&user).Update("status", common.UserStatusDisabled).Error)
			case "demoted":
				require.NoError(t, model.DB.Model(&user).Update("role", common.RoleAdminUser).Error)
			case "pat":
				token = "pat_pppppppppppppppppppppppppppppppppppppppppppppppp"
				require.NoError(t, model.DB.Create(&model.PersonalAccessToken{UserID: user.Id, Name: "pricing", Digest: model.PersonalAccessTokenDigest(token), Scopes: []string{"models:read"}, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
			}
			module := tc.module
			if module == "" {
				module = "pricing"
			}
			handled := false
			router := gin.New()
			router.GET("/api/"+module, HeaderNavModuleAuth(module), func(c *gin.Context) {
				handled = true
				assert.Equal(t, user.Id, c.GetInt("id"))
				assert.Equal(t, common.RoleRootUser, c.GetInt("role"))
				_, session := GetSessionAuthIdentity(c)
				assert.True(t, session)
				c.JSON(http.StatusOK, gin.H{"success": true})
			})
			request := httptest.NewRequest(http.MethodGet, "/api/"+module, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, tc.wantStatus, response.Code, response.Body.String())
			assert.Equal(t, tc.wantStatus == http.StatusOK, handled)
			if tc.credential == "pat" {
				assert.Contains(t, response.Body.String(), "PAT_SCOPE_DENIED")
			}
		})
	}
}

func TestHeaderNavModuleAuthRequiresLoginForPricing(t *testing.T) {
	raw := `{"pricing":{"enabled":true,"requireAuth":true}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModuleAuthRequiresLoginForRankings(t *testing.T) {
	raw := `{"rankings":{"enabled":true,"requireAuth":true}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("rankings"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModuleAuthRejectsLegacyDisabledModule(t *testing.T) {
	raw := `{"rankings":false}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModuleAuth("rankings"), false)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthAllowsDefaultPublicAccess(t *testing.T) {
	withHeaderNavModules(t, "")

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthRequiresLoginWhenDisabled(t *testing.T) {
	raw := `{"pricing":{"enabled":false,"requireAuth":false}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthAllowsLoggedInWhenDisabled(t *testing.T) {
	raw := `{"pricing":{"enabled":false,"requireAuth":false}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), true)

	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthRequiresLoginWhenRequireAuth(t *testing.T) {
	raw := `{"pricing":{"enabled":true,"requireAuth":true}}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavModulePublicOrUserAuthRequiresLoginForLegacyDisabledModule(t *testing.T) {
	raw := `{"pricing":false}`
	withHeaderNavModules(t, raw)

	recorder := performHeaderNavRequest(t, HeaderNavModulePublicOrUserAuth("pricing"), false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}

func TestHeaderNavPublicRouteRejectsExpiredInternalAccessToken(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	withHeaderNavModules(t, "")
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.GET("/api/test", HeaderNavModuleAuth("pricing"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	request.Header.Set("Authorization", "Bearer "+issueExpiredDashboardAccessToken(t, service.AuthIdentity{
		UserID: 1, SessionID: "expired-header-nav-session", UserAuthVersion: 1, SessionVersion: 1,
	}))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Contains(t, response.Body.String(), "AUTH_TOKEN_EXPIRED")
}
