package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A model catalog filtered by token_id shows that Key's model limits and
// routing, the same projection as /api/token/:id/routing/channels. A scoped
// personal access token therefore needs routing:read for it, like that route;
// profile:read alone still reads the unfiltered catalog, and a browser session
// is unchanged.
func TestModelCatalogForAKeyRequiresRoutingReadScope(t *testing.T) {
	setupRelayRouterTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.UserSession{}, &model.UserAccessToken{}, &model.AuditLog{}))
	previousSecret := common.SessionSecret
	common.SessionSecret = "catalog-scope-test-secret"
	t.Cleanup(func() { common.SessionSecret = previousSecret })
	engine := gin.New()
	SetApiRouter(engine)
	user, session := createSystemStatusSession(t, db, 91201, "catalog-scope", common.RoleCommonUser)
	key := model.Token{UserId: user.Id, Key: "catalogscopekey", Name: "limited", Status: common.TokenStatusEnabled, ExpiredTime: -1, ModelLimitsEnabled: true, ModelLimits: "private-model"}
	require.NoError(t, db.Create(&key).Error)
	channel := model.Channel{Type: 1, Key: "provider-key", Name: "catalog-channel", Status: common.ChannelStatusEnabled, Models: "private-model,other-model", ChannelRatio: "1"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	pat := func(scopes ...string) string {
		suffix, err := common.GenerateRandomCharsKey(43)
		require.NoError(t, err)
		raw := model.AccessTokenPrefix + suffix
		token := &model.UserAccessToken{Name: "catalog", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
		require.NoError(t, token.SetScopes(scopes))
		require.NoError(t, model.CreateUserAccessToken(user.Id, token, service.AccessTokenMaxPerUser))
		return raw
	}
	catalog := func(bearer, query string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet, "/api/user/model-catalog"+query, nil)
		request.Header.Set("Authorization", "Bearer "+bearer)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response
	}
	forKey := "?token_id=" + strconv.Itoa(key.Id)
	profileOnly := pat("profile:read")
	withRouting := pat("profile:read", "routing:read")

	denied := catalog(profileOnly, forKey)
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	require.Contains(t, denied.Body.String(), `"code":"ACCESS_TOKEN_SCOPE_DENIED"`)
	require.NotContains(t, denied.Body.String(), "private-model")
	for name, response := range map[string]*httptest.ResponseRecorder{
		"profile and routing scopes": catalog(withRouting, forKey),
		"browser session":            catalog(session, forKey),
		"profile scope without key":  catalog(profileOnly, ""),
	} {
		require.Equal(t, http.StatusOK, response.Code, name+": "+response.Body.String())
		require.Contains(t, response.Body.String(), `"success":true`, name)
	}
}
