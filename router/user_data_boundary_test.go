package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserDataRoutesKeepIdentitySecretsAndPageBounds(t *testing.T) {
	setupRelayRouterTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.UserSession{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.Log{}))
	previousSecret := common.SessionSecret
	common.SessionSecret = "user-data-boundary-test"
	t.Cleanup(func() { common.SessionSecret = previousSecret })
	user, bearer := createSystemStatusSession(t, db, 92001, "probe%", common.RoleCommonUser)
	admin, adminBearer := createSystemStatusSession(t, db, 92002, "boundary-admin", common.RoleAdminUser)
	root := model.User{Id: 99999, Username: "secret-root", AffCode: "secret-root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, Setting: `{"language":"zh","accept_unset_model_ratio_model":true,"webhook_secret":"secret-webhook","webhook_url":"https://secret-endpoint.example/path?key=secret-query","bark_url":"https://bark.example/secret-bark","gotify_token":"secret-gotify","gotify_url":"https://user:secret-pass@g.example/"}`}
	require.NoError(t, db.Create(&root).Error)
	for i := 0; i < 105; i++ {
		require.NoError(t, db.Create(&model.User{Username: fmt.Sprintf("page-%d", i), AffCode: fmt.Sprintf("page-%d", i), Status: common.UserStatusEnabled}).Error)
	}
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: user.Id, Username: "former-name", CreatedAt: time.Now().Unix(), Type: model.LogTypeConsume, Quota: 7, PromptTokens: 3},
		{UserId: admin.Id, Username: "probe-other", CreatedAt: time.Now().Unix(), Type: model.LogTypeConsume, Quota: 99, PromptTokens: 100},
	}).Error)
	engine := gin.New()
	SetApiRouter(engine)
	get := func(token, path string) (*httptest.ResponseRecorder, map[string]any) {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		var body map[string]any
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body), response.Body.String())
		return response, body
	}
	t.Run("self stat identity", func(t *testing.T) {
		r, body := get(bearer, "/api/log/self/stat?username=probe-other&user_id=92002")
		require.Equal(t, http.StatusOK, r.Code, r.Body.String())
		require.Equal(t, true, body["success"])
		data := body["data"].(map[string]any)
		require.EqualValues(t, 7, data["quota"])
		require.EqualValues(t, 1, data["rpm"])
		require.EqualValues(t, 3, data["tpm"])
	})
	t.Run("list secrets", func(t *testing.T) {
		suffix, err := common.GenerateRandomCharsKey(43)
		require.NoError(t, err)
		raw := model.AccessTokenPrefix + suffix
		pat := &model.UserAccessToken{Name: "read", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
		require.NoError(t, pat.SetScopes([]string{"user:read"}))
		require.NoError(t, model.CreateUserAccessToken(admin.Id, pat, service.AccessTokenMaxPerUser))
		for _, token := range []string{adminBearer, raw} {
			for _, path := range []string{"/api/user/?sort_by=id&sort_order=asc", "/api/user/search?keyword=secret-root"} {
				r, body := get(token, path)
				require.Equal(t, http.StatusOK, r.Code, r.Body.String())
				require.Equal(t, true, body["success"])
				for _, secret := range []string{"secret-webhook", "secret-query", "secret-bark", "secret-gotify", "secret-pass", "secret-endpoint"} {
					require.NotContains(t, r.Body.String(), secret)
				}
				require.Contains(t, r.Body.String(), "accept_unset_model_ratio_model")
			}
		}
		var persisted model.User
		require.NoError(t, db.First(&persisted, root.Id).Error)
		require.Equal(t, root.Setting, persisted.Setting, "projection never rewrites stored settings")
	})
	t.Run("negative page size", func(t *testing.T) {
		for _, query := range []string{"page_size=-1", "ps=-1", "size=-1"} {
			r, body := get(adminBearer, "/api/user/?"+query)
			require.Equal(t, http.StatusOK, r.Code, r.Body.String())
			data := body["data"].(map[string]any)
			require.LessOrEqual(t, len(data["items"].([]any)), 100)
			require.GreaterOrEqual(t, data["page_size"].(float64), float64(1))
		}
	})
}
