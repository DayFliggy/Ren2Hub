package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B6：用 PAT 创建手动路由 Token 时，PAT 自己也必须带 routing:write，只看用户角色不够。
func TestAddManualRoutingTokenRequiresPATRoutingScope(t *testing.T) {
	user, _ := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleCommonUser).Error)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}))
	router := newAccessTokenTestRouter()
	router.POST("/api/token/", middleware.UserAuth(), AddToken)
	keyOnly, _ := createScopedAccessToken(t, user.Id, 0, "api_key:write")
	withRouting, _ := createScopedAccessToken(t, user.Id, 0, "api_key:write", "routing:write")
	_, browser := createAccessTokenTestSession(t, user.Id, "manual-routing-session")
	manual := func(name string) string { return `{"name":"` + name + `","type":"manual","unlimited_quota":true}` }

	denied := accessTokenRequest(router, http.MethodPost, "/api/token/", keyOnly, "", manual("pat-without-routing"))
	assert.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	assert.Contains(t, denied.Body.String(), "ROUTING_FORBIDDEN")

	auto := accessTokenRequest(router, http.MethodPost, "/api/token/", keyOnly, "", `{"name":"pat-auto","type":"auto","unlimited_quota":true}`)
	assert.Contains(t, auto.Body.String(), `"success":true`, auto.Body.String())

	scoped := accessTokenRequest(router, http.MethodPost, "/api/token/", withRouting, "", manual("pat-with-routing"))
	assert.Contains(t, scoped.Body.String(), `"success":true`, scoped.Body.String())

	session := accessTokenRequest(router, http.MethodPost, "/api/token/", browser, "", manual("browser-session"))
	assert.Contains(t, session.Body.String(), `"success":true`, session.Body.String())

	var names []string
	require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Order("name").Pluck("name", &names).Error)
	assert.Equal(t, []string{"browser-session", "pat-auto", "pat-with-routing"}, names)
	var manualProfiles int64
	require.NoError(t, model.DB.Model(&model.TokenRouteProfile{}).Where("mode = ?", model.TokenRouteManual).Count(&manualProfiles).Error)
	assert.EqualValues(t, 2, manualProfiles)
}
