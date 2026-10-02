package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpsMonitorRoutesFollowAdminChannelAndRootBoundaries(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AuditLog{}, &model.CasbinRule{}, &model.AuthzRole{},
		&model.ChannelHealth{}, &model.PerfMetric{}, &model.PerfMetricFlushReceipt{}, &model.SystemTask{}))
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	require.NoError(t, authz.Init(model.DB))
	common.IsMasterNode = wasMaster
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
	access := map[int]string{}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		name := fmt.Sprintf("ops-%d", role)
		require.NoError(t, model.DB.Create(&model.User{Id: role, Username: name, AffCode: name, Role: role, Status: common.UserStatusEnabled}).Error)
		access[role] = createRouterBrowserSession(t, role)
	}
	engine := gin.New()
	SetApiRouter(engine)
	call := func(role int, path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if role != 0 {
			request.Header.Set("Authorization", "Bearer "+access[role])
		}
		engine.ServeHTTP(recorder, request)
		return recorder
	}

	adminPaths := []string{"/api/ops/overview", "/api/ops/requests", "/api/ops/channels"}
	for _, path := range append(adminPaths, "/api/system-task/summary", "/api/gateway/metrics") {
		assert.Equal(t, http.StatusUnauthorized, call(0, path).Code, path)
		assert.Equal(t, http.StatusForbidden, call(common.RoleCommonUser, path).Code, path)
	}
	for _, path := range adminPaths {
		response := call(common.RoleAdminUser, path)
		require.Equal(t, http.StatusOK, response.Code, path)
		assert.Contains(t, response.Header().Get("Cache-Control"), "no-store", path)
	}
	assert.Equal(t, http.StatusForbidden, call(common.RoleAdminUser, "/api/system-task/summary").Code,
		"task history can contain internal errors and stays Root-only")
	assert.Equal(t, http.StatusForbidden, call(common.RoleAdminUser, "/api/gateway/metrics").Code,
		"billing counters and recovery backlog remain Root-only")

	requests := call(common.RoleAdminUser, "/api/ops/requests?hours=24")
	assert.Contains(t, requests.Body.String(), `"success":true`)
	assert.Contains(t, requests.Body.String(), `"bucket_seconds"`)
	for _, hours := range []string{"0", "169", "day"} {
		assert.Contains(t, call(common.RoleAdminUser, "/api/ops/requests?hours="+hours).Body.String(), `"success":false`, hours)
	}
	assert.Contains(t, call(common.RoleAdminUser, "/api/ops/overview?window=7d").Body.String(), `"success":false`)
	assert.Contains(t, call(common.RoleAdminUser, "/api/ops/channels").Body.String(), `"breakers"`)
	require.NoError(t, authz.SetUserPermissions(common.RoleAdminUser, authz.PermissionsMap{
		authz.ResourceChannel: {authz.ActionRead: false},
	}))
	assert.Equal(t, http.StatusForbidden, call(common.RoleAdminUser, "/api/ops/channels").Code,
		"an admin role does not bypass an explicit channel.read denial")
	assert.Equal(t, http.StatusOK, call(common.RoleAdminUser, "/api/ops/requests").Code)
	assert.Equal(t, http.StatusOK, call(common.RoleRootUser, "/api/ops/channels").Code)

	summary := call(common.RoleRootUser, "/api/system-task/summary")
	require.Equal(t, http.StatusOK, summary.Code)
	assert.Contains(t, summary.Body.String(), `"type":"log_cleanup"`)

	for _, retired := range []string{"/api/system-info/instances", "/api/system-info/stale-instances"} {
		assert.Equal(t, http.StatusNotFound, call(common.RoleRootUser, retired).Code, retired)
	}
}
