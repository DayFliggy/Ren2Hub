package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestManagementOptionsRejectRetiredRoutingBeforePersistence(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, key := range []string{"GroupRatio", "TopupGroupRatio", "ModelRequestRateLimitEnabled", "ModelRequestRateLimitGroup", "AutoGroups", "group_ratio_setting.group_ratio"} {
		t.Run(key, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option", strings.NewReader(`{"key":"`+key+`","value":"{}"}`))
			UpdateOption(ctx)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), `"code":"retired_field"`)
			require.Contains(t, recorder.Body.String(), key)
		})
	}
}

func TestManagementAffinityRulesRejectRetiredRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option", strings.NewReader(`{"key":"channel_affinity_setting.rules","value":"[{\"name\":\"old\",\"include_using_group\":false}]"}`))
	UpdateOption(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"retired_field"`)
}

func TestManagementBulkRejectsRetiredRoutingBeforeOtherChanges(t *testing.T) {
	withOptionMap(t, map[string]string{"SystemName": "before"})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/api/option/bulk", strings.NewReader(`{"options":{"SystemName":"after","GroupRatio":{"vip":0}}}`))
	UpdateOptionsBulk(ctx)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"code":"retired_field"`)
	require.Equal(t, "before", optionSnapshot(nil)["SystemName"])
}
