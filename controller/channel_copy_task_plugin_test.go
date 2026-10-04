package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Copying a channel binds the same task plugins, so it needs the permission
// that adding such a channel requires.
func TestCopyChannelWithTaskPluginBindingRequiresBindPermission(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.Log{}, &model.AuditLog{}))
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = wasMaster })
	require.NoError(t, authz.Init(model.DB))

	admin := &model.User{Username: "copy-admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffCode: "copy-admin"}
	require.NoError(t, db.Create(admin).Error)
	settingBytes, err := common.Marshal(dto.ChannelSettings{TaskExtendPluginKeys: []string{"kling"}})
	require.NoError(t, err)
	setting := string(settingBytes)
	origin := &model.Channel{Type: constant.ChannelTypeNewAPI, Name: "gateway", Key: "test-key", Models: "kling-v1", Group: "default", Setting: &setting}
	require.NoError(t, db.Create(origin).Error)

	copyAs := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", admin.Id)
		ctx.Set("role", admin.Role)
		ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", origin.Id)}}
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/copy", nil)
		CopyChannel(ctx)
		return recorder
	}
	countChannels := func() int64 {
		var count int64
		require.NoError(t, db.Model(&model.Channel{}).Count(&count).Error)
		return count
	}

	assert.Equal(t, http.StatusForbidden, copyAs().Code)
	assert.EqualValues(t, 1, countChannels())

	require.NoError(t, authz.SetUserPermissions(admin.Id, authz.PermissionsMap{authz.ResourceTaskPlugin: {authz.ActionBind: true}}))
	recorder := copyAs()
	assert.Contains(t, recorder.Body.String(), `"success":true`)
	assert.EqualValues(t, 2, countChannels())
}
