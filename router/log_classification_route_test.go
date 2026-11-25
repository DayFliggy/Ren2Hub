package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogClassificationRoutesAndPermissions(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Log{}, &model.Midjourney{}, &model.Task{}, &model.AuditLog{}))

	member, memberBearer := createSystemStatusSession(t, db, 94101, "member-log", common.RoleCommonUser)
	_, rootBearer := createSystemStatusSession(t, db, 94102, "root-log", common.RoleRootUser)
	require.NoError(t, db.Create(&model.Channel{Id: 5, Name: "chan-5"}).Error)

	now := time.Now().Unix()
	require.NoError(t, db.Create(&model.Log{UserId: member.Id, Username: member.Username, CreatedAt: now - 20, Type: model.LogTypeConsume, ModelName: "gpt-4o", ChannelId: 5, Quota: 10, RequestId: "r-call"}).Error)
	require.NoError(t, db.Create(&model.Log{UserId: member.Id, Username: member.Username, CreatedAt: now - 10, Type: model.LogTypeSystem, Quota: 0, RequestId: "r-sys"}).Error)
	require.NoError(t, db.Create(&model.Midjourney{UserId: member.Id, MjId: "mj-1", Action: "IMAGINE", Status: "FAILURE", SubmitTime: (now - 10) * 1000, ChannelId: 5}).Error)
	require.NoError(t, db.Create(&model.AuditLog{EventId: "e-1", UserId: member.Id, Username: member.Username, ActorRole: common.RoleCommonUser, CreatedAt: now - 10, Category: model.AuditCategoryLogin, Action: "login", Success: true, Status: 200}).Error)
	require.NoError(t, db.Create(&model.AuditLog{EventId: "e-2", UserId: member.Id, Username: member.Username, ActorRole: common.RoleCommonUser, CreatedAt: now - 5, Category: model.AuditCategoryAccessToken, Action: "", Success: true, Status: 200}).Error)

	doGet := func(path string, bearer string) (*httptest.ResponseRecorder, map[string]any) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		var payload map[string]any
		_ = common.Unmarshal(w.Body.Bytes(), &payload)
		return w, payload
	}

	// Regular user cannot access admin all-account log endpoints.
	for _, adminPath := range []string{"/api/log/?types=2,5", "/api/mj/?status=FAILURE", "/api/task?status=FAILURE", "/api/audit?categories=login,security"} {
		w, payload := doGet(adminPath, memberBearer)
		assert.NotEqual(t, true, payload["success"], "member must be denied %s (status=%d)", adminPath, w.Code)
	}

	// Regular user self endpoints honor multi-type filter.
	w, payload := doGet("/api/log/self?types=2,5", memberBearer)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, true, payload["success"])
	items := payload["data"].(map[string]any)["items"].([]any)
	require.Len(t, items, 1)
	assert.Equal(t, "r-call", items[0].(map[string]any)["request_id"])

	// Invalid types fail closed through real router.
	_, badPayload := doGet("/api/log/self?types=2,99", memberBearer)
	assert.Equal(t, false, badPayload["success"])

	// Root can query all-account endpoints with classification filters.
	w, payload = doGet("/api/log/?types=1,4,6&username=member-log", rootBearer)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, true, payload["success"])
	quotaItems := payload["data"].(map[string]any)["items"].([]any)
	require.Len(t, quotaItems, 1)
	assert.Equal(t, "r-sys", quotaItems[0].(map[string]any)["request_id"])

	w, payload = doGet("/api/mj/?status=FAILURE", rootBearer)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, true, payload["success"])
	mjItems := payload["data"].(map[string]any)["items"].([]any)
	require.Len(t, mjItems, 1)
	assert.Equal(t, "mj-1", mjItems[0].(map[string]any)["mj_id"])

	w, payload = doGet("/api/audit?categories=login,security", rootBearer)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, true, payload["success"])
	auditItems := payload["data"].(map[string]any)["items"].([]any)
	require.Len(t, auditItems, 1)
	assert.Equal(t, "e-1", auditItems[0].(map[string]any)["event_id"])
}
