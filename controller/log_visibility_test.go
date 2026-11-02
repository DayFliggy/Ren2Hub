package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLogsSelfStatScopesByIDAcrossWildcardsAndRenames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	now := time.Now().Unix()
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: 1, Username: "ab%", CreatedAt: now - 5, Type: model.LogTypeConsume, Quota: 1, PromptTokens: 2, CompletionTokens: 3},
		{UserId: 1, Username: "old-name", CreatedAt: now - 120, Type: model.LogTypeConsume, Quota: 10, PromptTokens: 10},
		{UserId: 2, Username: "abalone", CreatedAt: now - 5, Type: model.LogTypeConsume, Quota: 100, PromptTokens: 100},
		{UserId: 3, Username: "abc", CreatedAt: now - 5, Type: model.LogTypeConsume, Quota: 1000, PromptTokens: 1000},
		// A reused name must never inherit the previous account's usage.
		{UserId: 4, Username: "ab%", CreatedAt: now - 5, Type: model.LogTypeConsume, Quota: 10000, PromptTokens: 10000},
	}).Error)
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		for _, username := range []string{"ab%", "%ai%", "ab_", "renamed"} {
			t.Run(fmt.Sprintf("role=%d/name=%s", role, username), func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Set("id", 1)
				ctx.Set("role", role)
				ctx.Set("username", username)
				ctx.Request = httptest.NewRequest(http.MethodGet, "/api/log/self/stat?user_id=2&username="+url.QueryEscape("%"), nil)
				GetLogsSelfStat(ctx)
				var response struct {
					Success bool       `json:"success"`
					Data    model.Stat `json:"data"`
				}
				require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
				require.True(t, response.Success)
				require.Equal(t, model.Stat{Quota: 11, Rpm: 1, Tpm: 5}, response.Data)
			})
		}
	}
	for _, id := range []int{0, -1} {
		_, err := model.SumUserUsedQuota(context.Background(), id, model.LogFilter{})
		require.Error(t, err)
	}
}

func TestAdminLogVisibilityWithSeparateLogDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mainDB, _ := newAuditTestDatabase(t, "sqlite", "")
	logDB, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, mainDB, logDB, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, mainDB.AutoMigrate(&model.User{}))
	require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	for _, user := range []model.User{
		{Id: 1, Username: "common", Role: common.RoleCommonUser, AffCode: "one"},
		{Id: 2, Username: "admin", Role: common.RoleAdminUser, AffCode: "two"},
		{Id: 3, Username: "root", Role: common.RoleRootUser, AffCode: "three"},
		{Id: 4, Username: "deleted-root", Role: common.RoleRootUser, AffCode: "four"},
	} {
		require.NoError(t, mainDB.Create(&user).Error)
	}
	require.NoError(t, mainDB.Delete(&model.User{}, 4).Error)
	now := time.Now().Unix()
	for i, name := range []string{"common", "admin", "root", "deleted-root"} {
		require.NoError(t, logDB.Create(&model.Log{
			UserId: i + 1, Username: name, CreatedAt: now - int64(10-i),
			Type: model.LogTypeConsume, Quota: (i + 1) * 100, PromptTokens: i + 1,
			RequestId: name, Other: `{"admin_info":{"visible":true},"root_info":{"private":true}}`,
		}).Error)
	}
	for _, tc := range []struct {
		role, count, quota, tokens int
		requests                   []any
	}{
		{common.RoleAdminUser, 2, 300, 3, []any{"admin", "common"}},
		{common.RoleRootUser, 4, 1000, 10, []any{"deleted-root", "root", "admin", "common"}},
	} {
		t.Run(fmt.Sprint(tc.role), func(t *testing.T) {
			listed := callLogHandler(t, GetAllLogs, 2, tc.role, "page_size=1")[0]
			require.Equal(t, true, listed["success"])
			data := listed["data"].(map[string]any)
			require.EqualValues(t, tc.count, data["total"])
			items := data["items"].([]any)
			require.Len(t, items, 1)
			require.Equal(t, tc.requests[0], items[0].(map[string]any)["request_id"])
			exported := callLogHandler(t, ExportAllLogs, 2, tc.role, "")
			require.Equal(t, tc.requests, exportedRequestIds(exported))
			require.Equal(t, true, exported[len(exported)-1]["success"])
			stat := callLogHandler(t, GetLogsStat, 2, tc.role, "")[0]
			require.Equal(t, true, stat["success"])
			require.Equal(t, map[string]any{"quota": float64(tc.quota), "rpm": float64(tc.count), "tpm": float64(tc.tokens)}, stat["data"])
		})
	}
	for _, query := range []string{"username=root", "username=%25root%25", "request_id=root"} {
		listed := callLogHandler(t, GetAllLogs, 2, common.RoleAdminUser, query)[0]
		require.Equal(t, true, listed["success"])
		require.Zero(t, listed["data"].(map[string]any)["total"])
		stat := callLogHandler(t, GetLogsStat, 2, common.RoleAdminUser, query)[0]
		require.Equal(t, map[string]any{"quota": float64(0), "rpm": float64(0), "tpm": float64(0)}, stat["data"])
		exported := callLogHandler(t, ExportAllLogs, 2, common.RoleAdminUser, query)
		require.Empty(t, exportedRequestIds(exported))
	}
	// A failed role lookup must not turn into an unfiltered log query.
	require.NoError(t, mainDB.Migrator().DropTable(&model.User{}))
	for _, handler := range []gin.HandlerFunc{GetAllLogs, GetLogsStat, ExportAllLogs} {
		response := callLogHandler(t, handler, 2, common.RoleAdminUser, "")
		require.Len(t, response, 1)
		require.Equal(t, false, response[0]["success"])
	}
}
