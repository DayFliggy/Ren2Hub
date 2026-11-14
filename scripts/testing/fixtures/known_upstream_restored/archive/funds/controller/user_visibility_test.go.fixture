package controller

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUserDirectoryVisibilityAppliesBeforePaginationAndSearch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.User{}))
	roles := []int{common.RoleRootUser, common.RoleAdminUser, common.RoleCommonUser, common.RoleAdminUser, common.RoleCommonUser, common.RoleCommonUser}
	for i, role := range roles {
		require.NoError(t, db.Create(&model.User{
			Id: i + 1, Username: fmt.Sprintf("account%d", i+1), Role: role,
			Password: "not-returned", AffCode: fmt.Sprint(i), Status: common.UserStatusEnabled,
			Email: fmt.Sprintf("account%d@example.test", i+1),
		}).Error)
	}
	require.NoError(t, db.Delete(&model.User{}, 6).Error)
	assertPage := func(handler gin.HandlerFunc, role int, query string, total int, ids ...int) {
		t.Helper()
		response := callLogHandler(t, handler, 2, role, query)[0]
		require.Equal(t, true, response["success"])
		data := response["data"].(map[string]any)
		require.EqualValues(t, total, data["total"])
		var actual []int
		if items, ok := data["items"].([]any); ok {
			for _, item := range items {
				user := item.(map[string]any)
				actual = append(actual, int(user["id"].(float64)))
				require.NotEqual(t, "not-returned", user["password"])
			}
		}
		require.Equal(t, ids, actual)
	}
	for _, handler := range []gin.HandlerFunc{GetAllUsers, SearchUsers} {
		assertPage(handler, common.RoleAdminUser, "sort_by=id&sort_order=asc&page_size=1&p=1", 3, 3)
		assertPage(handler, common.RoleAdminUser, "sort_by=id&sort_order=asc&page_size=1&p=2", 3, 5)
		assertPage(handler, common.RoleRootUser, "sort_by=id&sort_order=asc&page_size=20", 6, 1, 2, 3, 4, 5, 6)
	}
	assertPage(SearchUsers, common.RoleAdminUser, "keyword=account&sort_by=id&sort_order=asc", 3, 3, 5, 6)
	assertPage(SearchUsers, common.RoleAdminUser, "keyword=1", 0)
	assertPage(SearchUsers, common.RoleAdminUser, "keyword=account1@example.test", 0)
	assertPage(SearchUsers, common.RoleAdminUser, "role=10", 0)
	assertPage(SearchUsers, common.RoleAdminUser, "role=100", 0)
	assertPage(SearchUsers, common.RoleAdminUser, "role=1&status=-1", 1, 6)
	assertPage(SearchUsers, common.RoleRootUser, "role=10&sort_by=id&sort_order=asc", 2, 2, 4)
	assertPage(SearchUsers, common.RoleRootUser, "role=100", 1, 1)
}
