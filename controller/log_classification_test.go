package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func callJSONHandler(t *testing.T, handler gin.HandlerFunc, path string, userId int, role int) (int, map[string]any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
	ctx.Set("id", userId)
	ctx.Set("role", role)
	handler(ctx)
	var body map[string]any
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
	return recorder.Code, body
}

func extractLogRequestIDs(t *testing.T, body map[string]any) []string {
	t.Helper()
	require.Equal(t, true, body["success"], "response=%v", body)
	data, ok := body["data"].(map[string]any)
	require.True(t, ok)
	items, ok := data["items"].([]any)
	require.True(t, ok)
	ids := make([]string, 0, len(items))
	for _, raw := range items {
		row := raw.(map[string]any)
		ids = append(ids, fmt.Sprint(row["request_id"]))
	}
	return ids
}

func TestLogClassificationFilterDatabaseMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = previousMaster })

	for _, tc := range []struct {
		kind, env string
		typ       common.DatabaseType
	}{
		{"sqlite", "", common.DatabaseTypeSQLite},
		{"mysql", "AUDIT_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres", "AUDIT_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			dsn := os.Getenv(tc.env)
			if tc.env != "" && dsn == "" {
				t.Skip(tc.env + " is not configured")
			}
			db, _ := newAuditTestDatabase(t, tc.kind, dsn)
			useLogExportDatabases(t, db, db, tc.typ, tc.typ)
			require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.User{}, &model.Log{}, &model.Midjourney{}, &model.AuditLog{}))

			now := time.Now().Unix()
			require.NoError(t, db.Create(&model.Channel{Id: 9, Name: "primary"}).Error)
			require.NoError(t, db.Create(&model.User{Id: 1, Username: "alice", Role: common.RoleCommonUser, AffCode: "alice"}).Error)
			require.NoError(t, db.Create(&model.User{Id: 2, Username: "admin", Role: common.RoleAdminUser, AffCode: "admin"}).Error)

			for _, row := range []model.Log{
				{UserId: 1, Username: "alice", CreatedAt: now - 50, Type: model.LogTypeConsume, ModelName: "gpt-4o", ChannelId: 9, Quota: 100, RequestId: "req-consume"},
				{UserId: 1, Username: "alice", CreatedAt: now - 40, Type: model.LogTypeError, ModelName: "gpt-4o", ChannelId: 9, Quota: 0, RequestId: "req-error"},
				{UserId: 1, Username: "alice", CreatedAt: now - 30, Type: model.LogTypeTopup, Quota: 0, RequestId: "req-topup", Content: "topup"},
				{UserId: 1, Username: "alice", CreatedAt: now - 20, Type: model.LogTypeRefund, ModelName: "sora-2", Quota: 50, RequestId: "req-refund", Content: "refund"},
				{UserId: 1, Username: "alice", CreatedAt: now - 10, Type: model.LogTypeSystem, Quota: 0, RequestId: "req-system", Content: "checkin"},
			} {
				require.NoError(t, db.Create(&row).Error)
			}

			// 1. Call logs multi-type filter (consume + error)
			_, callsBody := callJSONHandler(t, GetUserLogs, "/api/log/self?types=2,5", 1, common.RoleCommonUser)
			assert.ElementsMatch(t, []string{"req-consume", "req-error"}, extractLogRequestIDs(t, callsBody))

			// 2. Quota ledger multi-type filter (topup + system + refund)
			_, quotaBody := callJSONHandler(t, GetUserLogs, "/api/log/self?types=1,4,6", 1, common.RoleCommonUser)
			assert.ElementsMatch(t, []string{"req-topup", "req-refund", "req-system"}, extractLogRequestIDs(t, quotaBody))

			// 3. Explicit single type takes precedence over types list
			_, singleBody := callJSONHandler(t, GetUserLogs, "/api/log/self?type=4&types=1,4,6", 1, common.RoleCommonUser)
			assert.Equal(t, []string{"req-system"}, extractLogRequestIDs(t, singleBody))

			// 4. Invalid types rejected cleanly on list and export
			_, badList := callJSONHandler(t, GetUserLogs, "/api/log/self?types=2,99", 1, common.RoleCommonUser)
			assert.Equal(t, false, badList["success"])
			_, badText := callJSONHandler(t, GetAllLogs, "/api/log/?types=abc", 2, common.RoleAdminUser)
			assert.Equal(t, false, badText["success"])
			badExport := callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, "types=0,2")
			require.Len(t, badExport, 1)
			assert.Equal(t, false, badExport[0]["success"])

			// 5. Streaming export honors multi-type filter
			exported := callLogHandler(t, ExportUserLogs, 1, common.RoleCommonUser, "types=1,4,6")
			assert.ElementsMatch(t, []any{"req-topup", "req-refund", "req-system"}, exportedRequestIds(exported))

			// 6. Midjourney status filter works for user & admin views
			for _, task := range []model.Midjourney{
				{UserId: 1, MjId: "mj-ok", Action: "IMAGINE", Status: "SUCCESS", SubmitTime: (now - 30) * 1000, ChannelId: 9, Quota: 500},
				{UserId: 1, MjId: "mj-fail", Action: "IMAGINE", Status: "FAILURE", SubmitTime: (now - 20) * 1000, ChannelId: 9, Quota: 0},
				{UserId: 2, MjId: "mj-admin-fail", Action: "UPSCALE", Status: "FAILURE", SubmitTime: (now - 10) * 1000, ChannelId: 9, Quota: 0},
			} {
				require.NoError(t, db.Create(&task).Error)
			}
			_, mjSelfBody := callJSONHandler(t, GetUserMidjourney, "/api/mj/self?status=FAILURE", 1, common.RoleCommonUser)
			require.Equal(t, true, mjSelfBody["success"])
			mjSelfItems := mjSelfBody["data"].(map[string]any)["items"].([]any)
			require.Len(t, mjSelfItems, 1)
			assert.Equal(t, "mj-fail", mjSelfItems[0].(map[string]any)["mj_id"])

			_, mjAdminBody := callJSONHandler(t, GetAllMidjourney, "/api/mj/?status=FAILURE", 2, common.RoleAdminUser)
			require.Equal(t, true, mjAdminBody["success"])
			mjAdminItems := mjAdminBody["data"].(map[string]any)["items"].([]any)
			assert.Len(t, mjAdminItems, 2)

			// 7. Audit log multi-category filter (login + security, excluding noisy access_token)
			for _, entry := range []model.AuditLog{
				{EventId: "evt-login", UserId: 1, Username: "alice", ActorRole: common.RoleCommonUser, CreatedAt: now - 40, Category: model.AuditCategoryLogin, Action: "login", Success: true, Status: 200},
				{EventId: "evt-sec", UserId: 1, Username: "alice", ActorRole: common.RoleCommonUser, CreatedAt: now - 30, Category: model.AuditCategorySecurity, Action: "user.password_change", Success: true, Status: 200},
				{EventId: "evt-pat", UserId: 1, Username: "alice", ActorRole: common.RoleCommonUser, CreatedAt: now - 20, Category: model.AuditCategoryAccessToken, Action: "", Success: true, Status: 200},
				{EventId: "evt-op", UserId: 2, Username: "admin", ActorRole: common.RoleAdminUser, CreatedAt: now - 10, Category: model.AuditCategoryOperation, Action: "channel.update", Success: true, Status: 200},
			} {
				require.NoError(t, db.Create(&entry).Error)
			}

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/audit/self?categories=login,security", nil)
			ctx.Set("id", 1)
			ctx.Set("role", common.RoleCommonUser)
			// Match GetAuditLogs self-view path check via a lightweight router
			engine := gin.New()
			engine.GET("/api/audit/self", func(c *gin.Context) {
				c.Set("id", 1)
				c.Set("role", common.RoleCommonUser)
				GetAuditLogs(c)
			})
			engine.GET("/api/audit", func(c *gin.Context) {
				c.Set("id", 2)
				c.Set("role", common.RoleAdminUser)
				GetAuditLogs(c)
			})
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/audit/self?categories=login,security", nil))
			var auditBody map[string]any
			require.NoError(t, common.Unmarshal(w.Body.Bytes(), &auditBody))
			require.Equal(t, true, auditBody["success"])
			auditItems := auditBody["data"].(map[string]any)["items"].([]any)
			require.Len(t, auditItems, 2)
			eventIDs := []string{
				auditItems[0].(map[string]any)["event_id"].(string),
				auditItems[1].(map[string]any)["event_id"].(string),
			}
			assert.ElementsMatch(t, []string{"evt-login", "evt-sec"}, eventIDs)

			// Invalid categories must be rejected
			wBad := httptest.NewRecorder()
			engine.ServeHTTP(wBad, httptest.NewRequest(http.MethodGet, "/api/audit/self?categories=login,bogus", nil))
			var badAuditBody map[string]any
			require.NoError(t, common.Unmarshal(wBad.Body.Bytes(), &badAuditBody))
			assert.Equal(t, false, badAuditBody["success"])
		})
	}
}
