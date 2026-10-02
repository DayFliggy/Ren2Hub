package router

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func setupPATChannelPermissions(t *testing.T) (*gin.Engine, *model.User) {
	t.Helper()
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.PersonalAccessToken{}, &model.CasbinRule{}))
	previousCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousCache })
	owner := &model.User{Username: "pat-channel-owner", AffCode: "pat-channel-owner", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(owner).Error)
	engine := gin.New()
	declarePATRoutes()
	registerChannelRoutes(engine.Group("/api"))
	return engine, owner
}

func createChannelPermissionPAT(t *testing.T, ownerID int, scopes []string) string {
	t.Helper()
	var count int64
	require.NoError(t, model.DB.Model(&model.PersonalAccessToken{}).Count(&count).Error)
	raw := fmt.Sprintf("pat_%048d", count+1)
	require.NoError(t, model.DB.Create(&model.PersonalAccessToken{UserID: ownerID, Name: "channel-permission-test", Digest: model.PersonalAccessTokenDigest(raw), Scopes: scopes, ExpiresAt: time.Now().Add(time.Hour).Unix()}).Error)
	return raw
}

func requestChannelPermissionRoute(engine *gin.Engine, credential, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+credential)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func TestPATChannelWritesRequireEachDynamicScope(t *testing.T) {
	for _, scenario := range []struct {
		name, method, path, body string
		ordinary, additional     []string
		field, before, after     string
		configure                func(*model.Channel)
	}{
		{name: "credential", method: http.MethodPut, path: "/api/channel/", body: `{"id":%d,"key":"new-fixture-key"}`, ordinary: []string{"channels:write"}, additional: []string{"channels:sensitive_write"}, field: "key", before: "old-fixture-key", after: "new-fixture-key"},
		{name: "tag header override", method: http.MethodPut, path: "/api/channel/tag", body: `{"tag":"scope-fixture","header_override":"{\"X-Fixture\":\"changed\"}"}`, ordinary: []string{"channels:write"}, additional: []string{"channels:sensitive_write"}, field: "header_override", before: "", after: `{"X-Fixture":"changed"}`},
		{name: "remove credential", method: http.MethodPost, path: "/api/channel/multi_key/manage", body: `{"channel_id":%d,"action":"delete_key","key_index":0}`, ordinary: []string{"channels:operate"}, additional: []string{"channels:sensitive_write"}, field: "key", before: "first-fixture-key\nsecond-fixture-key", after: "second-fixture-key", configure: func(channel *model.Channel) {
			channel.Key = "first-fixture-key\nsecond-fixture-key"
			channel.ChannelInfo = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}
		}},
		{name: "remove plugin binding", method: http.MethodPut, path: "/api/channel/", body: `{"id":%d,"setting":"{}"}`, ordinary: []string{"channels:write", "channels:sensitive_write"}, additional: []string{"plugins:bind"}, field: "setting", before: `{"task_plugin_key":"retired-fixture"}`, after: "{}", configure: func(channel *model.Channel) {
			channel.Type = constant.ChannelTypeNewAPI
			channel.Setting = common.GetPointer(`{"task_plugin_key":"retired-fixture"}`)
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			engine, owner := setupPATChannelPermissions(t)
			channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "scope-fixture", Tag: common.GetPointer("scope-fixture"), Key: "old-fixture-key", Models: "model-fixture", Group: "default", BaseURL: common.GetPointer("https://api.example.test")}
			if scenario.configure != nil {
				scenario.configure(channel)
			}
			require.NoError(t, model.DB.Create(channel).Error)
			body := scenario.body
			if strings.Contains(body, "%d") {
				body = fmt.Sprintf(body, channel.Id)
			}
			ordinary := createChannelPermissionPAT(t, owner.Id, scenario.ordinary)
			denied := requestChannelPermissionRoute(engine, ordinary, scenario.method, scenario.path, body)
			require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
			assert.Equal(t, "PAT_SCOPE_DENIED", gjson.GetBytes(denied.Body.Bytes(), "code").String())
			var stored sql.NullString
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Select(scenario.field).Scan(&stored).Error)
			assert.Equal(t, scenario.before, stored.String)
			granted := createChannelPermissionPAT(t, owner.Id, append(append([]string(nil), scenario.ordinary...), scenario.additional...))
			accepted := requestChannelPermissionRoute(engine, granted, scenario.method, scenario.path, body)
			require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
			require.True(t, gjson.GetBytes(accepted.Body.Bytes(), "success").Bool(), accepted.Body.String())
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Select(scenario.field).Scan(&stored).Error)
			assert.True(t, stored.Valid)
			assert.Equal(t, scenario.after, stored.String)
		})
	}
}

func TestPATCannotBorrowRevokedOwnerPermission(t *testing.T) {
	engine, owner := setupPATChannelPermissions(t)
	owner.Role = common.RoleAdminUser
	require.NoError(t, model.DB.Model(owner).Update("role", owner.Role).Error)
	for _, permission := range []authz.Permission{authz.ChannelWrite, authz.ChannelSensitiveWrite} {
		require.NoError(t, model.DB.Create(&model.CasbinRule{Ptype: "p", V0: authz.UserSubject(owner.Id), V1: permission.Resource, V2: permission.Action, V3: authz.EffectAllow}).Error)
	}
	channel := &model.Channel{Type: constant.ChannelTypeOpenAI, Name: "owner-permission", Key: "old-fixture-key", Models: "model-fixture", Group: "default"}
	require.NoError(t, model.DB.Create(channel).Error)
	raw := createChannelPermissionPAT(t, owner.Id, []string{"channels:write", "channels:sensitive_write"})
	body := fmt.Sprintf(`{"id":%d,"key":"new-fixture-key"}`, channel.Id)
	accepted := requestChannelPermissionRoute(engine, raw, http.MethodPut, "/api/channel/", body)
	require.Equal(t, http.StatusOK, accepted.Code, accepted.Body.String())
	require.True(t, gjson.GetBytes(accepted.Body.Bytes(), "success").Bool(), accepted.Body.String())
	require.NoError(t, model.DB.Create(&model.CasbinRule{Ptype: "p", V0: authz.UserSubject(owner.Id), V1: authz.ChannelSensitiveWrite.Resource, V2: authz.ChannelSensitiveWrite.Action, V3: authz.EffectDeny}).Error)
	denied := requestChannelPermissionRoute(engine, raw, http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"key":"forbidden-fixture-key"}`, channel.Id))
	require.Equal(t, http.StatusForbidden, denied.Code, denied.Body.String())
	var stored model.Channel
	require.NoError(t, model.DB.First(&stored, channel.Id).Error)
	assert.Equal(t, "new-fixture-key", stored.Key)
}

func TestRequestPermissionKeepsBrowserPrivilegesAndRejectsUnmappedPAT(t *testing.T) {
	engine, owner := setupPATChannelPermissions(t)
	permission := authz.Permission{Resource: "future_resource", Action: "write"}
	middleware.DeclarePATRoute(http.MethodPost, "/api/scope-fixture", "channels:write")
	engine.POST("/api/scope-fixture", middleware.AdminAuth(), middleware.RequirePermission(permission), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	raw := createChannelPermissionPAT(t, owner.Id, []string{"channels:write", "channels:sensitive_write"})
	assert.Equal(t, http.StatusForbidden, requestChannelPermissionRoute(engine, raw, http.MethodPost, "/api/scope-fixture", "{}").Code)
	browser := createRouterBrowserSession(t, owner.Id)
	assert.Equal(t, http.StatusNoContent, requestChannelPermissionRoute(engine, browser, http.MethodPost, "/api/scope-fixture", "{}").Code)
}
