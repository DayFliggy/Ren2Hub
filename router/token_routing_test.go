package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenRoutingRoutesPermissionsAndCandidateEndpoint(t *testing.T) {
	setupRelayRouterTestDB(t)
	db := model.DB
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.UserSession{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	oldMaster := common.IsMasterNode
	common.IsMasterNode = true
	require.NoError(t, authz.Init(db))
	previousSecret := common.SessionSecret
	common.SessionSecret = "token-routing-router-test-secret"
	t.Cleanup(func() {
		common.SessionSecret = previousSecret
		common.IsMasterNode = oldMaster
	})

	engine := gin.New()
	SetApiRouter(engine)

	user, session := createSystemStatusSession(t, db, 91301, "routing-router-owner", common.RoleCommonUser)
	channel := model.Channel{Type: 1, Key: "provider-secret", Name: "router-channel", Status: common.ChannelStatusEnabled, Models: "gpt-4o", ChannelRatio: "1"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	pat := func(scopes ...string) string {
		suffix, err := common.GenerateRandomCharsKey(43)
		require.NoError(t, err)
		raw := model.AccessTokenPrefix + suffix
		token := &model.UserAccessToken{Name: strings.Join(scopes, ","), TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
		require.NoError(t, token.SetScopes(scopes))
		require.NoError(t, model.CreateUserAccessToken(user.Id, token, service.AccessTokenMaxPerUser))
		return raw
	}
	do := func(bearer, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+bearer)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}

	keyOnly := pat("api_key:write")
	readOnly := pat("routing:read")
	writeOnly := pat("routing:write")

	// api_key:write PAT 创建令牌并返回 data.id，但无权访问任何路由端点。
	created := do(keyOnly, http.MethodPost, "/api/token/", `{"name":"two-step","unlimited_quota":true}`)
	require.Equal(t, http.StatusOK, created.Code, created.Body.String())
	var createPayload struct {
		Success bool `json:"success"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(created.Body.Bytes(), &createPayload))
	require.True(t, createPayload.Success)
	require.Greater(t, createPayload.Data.ID, 0)
	tokenID := createPayload.Data.ID

	require.Equal(t, http.StatusForbidden, do(keyOnly, http.MethodGet, "/api/token/routing/channels", "").Code)
	require.Equal(t, http.StatusForbidden, do(keyOnly, http.MethodGet, fmt.Sprintf("/api/token/%d/routing", tokenID), "").Code)
	require.Equal(t, http.StatusForbidden, do(keyOnly, http.MethodPut, fmt.Sprintf("/api/token/%d/routing", tokenID), `{"mode":"auto","selected_only":false,"version":0,"entries":[]}`).Code)

	// routing:read 可读无 :id 候选端点和令牌路由，但不能写。
	candidates := do(readOnly, http.MethodGet, "/api/token/routing/channels?model_limits=gpt-4o", "")
	require.Equal(t, http.StatusOK, candidates.Code, candidates.Body.String())
	require.Contains(t, candidates.Body.String(), "router-channel")
	require.NotContains(t, candidates.Body.String(), "provider-secret")

	gotRoute := do(readOnly, http.MethodGet, fmt.Sprintf("/api/token/%d/routing", tokenID), "")
	require.Equal(t, http.StatusOK, gotRoute.Code, gotRoute.Body.String())
	require.Contains(t, gotRoute.Body.String(), `"route_scope":"platform"`)

	require.Equal(t, http.StatusForbidden, do(readOnly, http.MethodPut, fmt.Sprintf("/api/token/%d/routing", tokenID), fmt.Sprintf(`{"mode":"ordered","selected_only":true,"version":0,"entries":[{"channel_id":%d,"enabled":true}]}`, channel.Id)).Code)

	// routing:write 可写路由（含全部停用退回全平台的投影字段）。
	saved := do(writeOnly, http.MethodPut, fmt.Sprintf("/api/token/%d/routing", tokenID), fmt.Sprintf(`{"mode":"ordered","selected_only":true,"version":0,"entries":[{"channel_id":%d,"enabled":false}]}`, channel.Id))
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	require.Contains(t, saved.Body.String(), `"route_scope":"platform"`)
	require.Contains(t, saved.Body.String(), `"effective_mode":"auto"`)
	require.Contains(t, saved.Body.String(), `"enabled_route_channel_count":0`)

	// 静态预览端点已彻底移除（浏览器会话请求返回 404）。
	require.Equal(t, http.StatusNotFound, do(session, http.MethodGet, fmt.Sprintf("/api/token/%d/routing/preview?model=gpt-4o", tokenID), "").Code)
}
