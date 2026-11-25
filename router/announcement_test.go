package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementRoutesAuthAndPermissions(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })

	require.NoError(t, db.AutoMigrate(&model.Announcement{}, &model.AnnouncementRead{}, &model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))

	root, rootSession := createSystemStatusSession(t, db, 93101, "ann-root", common.RoleRootUser)
	_, adminSession := createSystemStatusSession(t, db, 93102, "ann-admin", common.RoleAdminUser)
	readOnlyAdmin, readOnlySession := createSystemStatusSession(t, db, 93103, "ann-readonly", common.RoleAdminUser)
	require.NoError(t, authz.SetUserPermissions(readOnlyAdmin.Id, authz.PermissionsMap{
		authz.ResourceAnnouncement: {authz.ActionRead: true, authz.ActionWrite: false},
	}))
	_, userSession := createSystemStatusSession(t, db, 93104, "ann-user", common.RoleCommonUser)

	patReadRaw := "nap_announcement-read-only"
	patRead := model.UserAccessToken{
		UserId:    root.Id,
		Name:      "ann read pat",
		TokenHash: model.AccessTokenFingerprint(patReadRaw),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	require.NoError(t, patRead.SetScopes([]string{"announcement:read"}))
	require.NoError(t, db.Create(&patRead).Error)

	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}

	// Unauthenticated blocked everywhere.
	assert.Equal(t, http.StatusUnauthorized, call("GET", "/api/announcements", "", "").Code)
	assert.Equal(t, http.StatusUnauthorized, call("GET", "/api/admin/announcements", "", "").Code)

	// Regular user cannot call admin announcement APIs.
	assert.Equal(t, http.StatusForbidden, call("GET", "/api/admin/announcements", userSession, "").Code)
	assert.Equal(t, http.StatusForbidden, call("POST", "/api/admin/announcements", userSession, `{"title":"x","content":"y"}`).Code)

	// Read-only admin can list, cannot create; PAT with announcement:read can list, cannot create or hit session-only user routes.
	assert.Equal(t, http.StatusOK, call("GET", "/api/admin/announcements", readOnlySession, "").Code)
	assert.Equal(t, http.StatusForbidden, call("POST", "/api/admin/announcements", readOnlySession, `{"title":"x","content":"y"}`).Code)
	assert.Equal(t, http.StatusOK, call("GET", "/api/admin/announcements", patReadRaw, "").Code)
	assert.Equal(t, http.StatusForbidden, call("POST", "/api/admin/announcements", patReadRaw, `{"title":"x","content":"y"}`).Code)
	assert.Equal(t, http.StatusForbidden, call("GET", "/api/announcements", patReadRaw, "").Code)

	// Admin creates one active popup announcement and one draft announcement.
	createActive := call("POST", "/api/admin/announcements", adminSession,
		`{"title":"系统维护","content":"请提前保存进度","type":"warning","status":"active","notify_mode":"popup","pinned":true}`)
	require.Equal(t, http.StatusOK, createActive.Code, createActive.Body.String())
	var createdResp struct {
		Success bool `json:"success"`
		Data    struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createActive.Body.Bytes(), &createdResp))
	require.True(t, createdResp.Success)
	activeID := createdResp.Data.ID

	createDraft := call("POST", "/api/admin/announcements", rootSession,
		`{"title":"草稿","content":"不公开","status":"draft"}`)
	require.Equal(t, http.StatusOK, createDraft.Code)
	var draftResp struct {
		Data struct {
			ID int `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(createDraft.Body.Bytes(), &draftResp))

	// User sees only active announcement and cannot mark draft read.
	userList := call("GET", "/api/announcements", userSession, "")
	require.Equal(t, http.StatusOK, userList.Code, userList.Body.String())
	assert.Contains(t, userList.Body.String(), `"unread_count":1`)
	assert.NotContains(t, userList.Body.String(), `"草稿"`)

	assert.Equal(t, http.StatusNotFound, call("POST", fmt.Sprintf("/api/announcements/%d/read", draftResp.Data.ID), userSession, "").Code)
	assert.Equal(t, http.StatusOK, call("POST", fmt.Sprintf("/api/announcements/%d/read", activeID), userSession, "").Code)

	// After marking read, unread_count drops to 0 and admin read-status reflects read_count=1.
	userListAfter := call("GET", "/api/announcements", userSession, "")
	require.Equal(t, http.StatusOK, userListAfter.Code)
	assert.Contains(t, userListAfter.Body.String(), `"unread_count":0`)

	readStatus := call("GET", fmt.Sprintf("/api/admin/announcements/%d/read-status", activeID), readOnlySession, "")
	require.Equal(t, http.StatusOK, readStatus.Code, readStatus.Body.String())
	assert.Contains(t, readStatus.Body.String(), `"read_count":1`)

	// Read-only admin cannot delete; full admin can delete.
	assert.Equal(t, http.StatusForbidden, call("DELETE", fmt.Sprintf("/api/admin/announcements/%d", activeID), readOnlySession, "").Code)
	assert.Equal(t, http.StatusOK, call("DELETE", fmt.Sprintf("/api/admin/announcements/%d", activeID), adminSession, "").Code)
}
