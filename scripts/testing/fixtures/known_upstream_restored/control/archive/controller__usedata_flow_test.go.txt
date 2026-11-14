package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type flowQuotaResponse struct {
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	Data    []model.FlowQuotaData `json:"data"`
}

func setupFlowControllerTestDB(t *testing.T) {
	t.Helper()
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Token{}, &model.QuotaData{}))
	require.NoError(t, model.DB.Create(&model.Channel{Id: 1, Name: "east"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 11, UserId: 1, Key: "sk-primary", Name: "primary"}).Error)
	require.NoError(t, model.DB.Create(&model.Token{Id: 22, UserId: 2, Key: "sk-backup", Name: "backup"}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    1,
		Username:  "alice",
		NodeName:  "node-a",
		TokenID:   11,
		ChannelID: 1,
		ModelName: "gpt-a",
		CreatedAt: 1100,
		Count:     2,
		Quota:     100,
		TokenUsed: 40,
	}).Error)
	require.NoError(t, model.DB.Create(&model.QuotaData{
		UserID:    2,
		Username:  "bob",
		NodeName:  "node-b",
		TokenID:   22,
		ChannelID: 1,
		ModelName: "gpt-b",
		CreatedAt: 1200,
		Count:     1,
		Quota:     70,
		TokenUsed: 30,
	}).Error)
}

func decodeFlowQuotaResponse(t *testing.T, recorder *httptest.ResponseRecorder) flowQuotaResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success, payload.Message)
	return payload
}

func TestGetAllFlowQuotaDatesUsesAdminDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleAdminUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=bob", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "bob", payload.Data[0].Username)
	require.Equal(t, "east", payload.Data[0].ChannelName)
	require.Empty(t, payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].NodeName)
}

func TestGetAllFlowQuotaDatesUsesRootDimensions(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("role", common.RoleRootUser)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow?start_timestamp=1000&end_timestamp=2000&username=alice", nil)

	GetAllFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "alice", payload.Data[0].Username)
	require.Equal(t, "node-a", payload.Data[0].NodeName)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Equal(t, "east", payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRestrictsToAuthenticatedUser(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=1000&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	payload := decodeFlowQuotaResponse(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Empty(t, payload.Data[0].Username)
	require.Equal(t, "primary", payload.Data[0].TokenName)
	require.Empty(t, payload.Data[0].ChannelName)
}

func TestGetUserFlowQuotaDatesRejectsInvalidTimeRange(t *testing.T) {
	setupFlowControllerTestDB(t)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/flow/self?start_timestamp=bad&end_timestamp=2000", nil)

	GetUserFlowQuotaDates(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload flowQuotaResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "invalid start_timestamp", payload.Message)
}

func TestAdminQuotaVisibilityProtectsRootAcrossLegacyEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.QuotaData{}))
	now := time.Now().Unix()
	for i, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser, common.RoleRootUser} {
		id := i + 1
		require.NoError(t, db.Create(&model.User{Id: id, Username: fmt.Sprintf("current-%d", id), Role: role, AffCode: fmt.Sprint(id)}).Error)
		require.NoError(t, db.Create(&model.QuotaData{
			UserID: id, Username: fmt.Sprintf("historical-%d", id), ModelName: "model", CreatedAt: now - 10,
			Count: id, Quota: id * 100, TokenUsed: id,
		}).Error)
	}
	require.NoError(t, db.Delete(&model.User{}, 4).Error)
	query := fmt.Sprintf("start_timestamp=%d&end_timestamp=%d", now-100, now)
	for _, handler := range []gin.HandlerFunc{GetQuotaDatesByUser, GetAllFlowQuotaDates} {
		for _, role := range []int{common.RoleAdminUser, common.RoleRootUser} {
			response := callLogHandler(t, handler, 2, role, query)[0]
			require.Equal(t, true, response["success"])
			rows := response["data"].([]any)
			if role == common.RoleAdminUser {
				require.Len(t, rows, 2)
			} else {
				require.Len(t, rows, 4)
			}
			for _, item := range rows {
				if role == common.RoleAdminUser {
					require.Contains(t, []string{"historical-1", "historical-2"}, item.(map[string]any)["username"])
				}
			}
		}
	}
	for _, handler := range []gin.HandlerFunc{GetAllQuotaDates, GetAllFlowQuotaDates} {
		for _, id := range []int{3, 4} {
			filter := query + fmt.Sprintf("&username=historical-%d", id)
			admin := callLogHandler(t, handler, 2, common.RoleAdminUser, filter)[0]
			require.Equal(t, true, admin["success"])
			require.Empty(t, admin["data"])
			root := callLogHandler(t, handler, 3, common.RoleRootUser, filter)[0]
			require.Equal(t, true, root["success"])
			require.Len(t, root["data"], 1)
		}
	}
	// Anonymous aggregates deliberately include all accounts, including Root.
	all := callLogHandler(t, GetAllQuotaDates, 2, common.RoleAdminUser, query)[0]
	require.Equal(t, true, all["success"])
	rows := all["data"].([]any)
	require.Len(t, rows, 1)
	require.EqualValues(t, 1000, rows[0].(map[string]any)["quota"])
	require.Empty(t, rows[0].(map[string]any)["username"])
	// A Root's own personal endpoint must still return their own history.
	self := callLogHandler(t, GetUserQuotaDates, 3, common.RoleRootUser, query)[0]
	require.Equal(t, true, self["success"])
	require.Len(t, self["data"], 1)
	// Lookup failures close every route that returns user-specific data.
	require.NoError(t, db.Migrator().DropTable(&model.User{}))
	for _, handler := range []gin.HandlerFunc{GetAllQuotaDates, GetQuotaDatesByUser, GetAllFlowQuotaDates} {
		response := callLogHandler(t, handler, 2, common.RoleAdminUser, query+"&username=historical-3")[0]
		require.Equal(t, false, response["success"])
	}
}
