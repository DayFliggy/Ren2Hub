package router

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogExportRoutesRequireOwnerScopeAndAdminRole(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.PersonalAccessToken{}, &model.CasbinRule{}))
	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	owner := &model.User{Username: "log-export-owner", AffCode: "log-export-owner", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(owner).Error)
	require.NoError(t, model.DB.Create(&[]model.Log{
		{UserId: owner.Id, Type: model.LogTypeConsume, CreatedAt: common.GetTimestamp(), RequestId: "owned-log"},
		{UserId: owner.Id + 1, Type: model.LogTypeConsume, CreatedAt: common.GetTimestamp(), RequestId: "other-log"},
	}).Error)
	engine := gin.New()
	SetApiRouter(engine)
	deniedToken := createChannelPermissionPAT(t, owner.Id, []string{"profile:read"})
	denied := requestChannelPermissionRoute(engine, deniedToken, http.MethodGet, "/api/log/self/export", "")
	assert.Equal(t, http.StatusForbidden, denied.Code)
	assert.Contains(t, denied.Body.String(), "PAT_SCOPE_DENIED")
	selfToken := createChannelPermissionPAT(t, owner.Id, []string{"logs:read"})
	self := requestChannelPermissionRoute(engine, selfToken, http.MethodGet, "/api/log/self/export", "")
	require.Equal(t, http.StatusOK, self.Code, self.Body.String())
	assert.Contains(t, self.Body.String(), "owned-log")
	assert.NotContains(t, self.Body.String(), "other-log")
	assert.Contains(t, self.Body.String(), `"count":1`)
	adminDenied := requestChannelPermissionRoute(engine, selfToken, http.MethodGet, "/api/log/export", "")
	assert.Equal(t, http.StatusForbidden, adminDenied.Code)

	require.NoError(t, model.DB.Model(owner).Update("role", common.RoleRootUser).Error)
	wrongAdminScope := requestChannelPermissionRoute(engine, selfToken, http.MethodGet, "/api/log/export", "")
	assert.Equal(t, http.StatusForbidden, wrongAdminScope.Code)
	adminToken := createChannelPermissionPAT(t, owner.Id, []string{"ops:read"})
	all := requestChannelPermissionRoute(engine, adminToken, http.MethodGet, "/api/log/export", "")
	require.Equal(t, http.StatusOK, all.Code, all.Body.String())
	assert.Contains(t, all.Body.String(), `"count":2`)
	assert.Contains(t, all.Body.String(), "other-log")
}
