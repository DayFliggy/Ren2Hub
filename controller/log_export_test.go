package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func logExportFixture(t *testing.T) *gin.Engine {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	conn.SetMaxOpenConns(1)
	oldDB, oldLogDB, oldType := model.DB, model.LOG_DB, common.LogDatabaseType()
	oldSettings := *operation_setting.GetLogSetting()
	model.DB, model.LOG_DB = db, db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	initNextAdminUserColumns(t)
	*operation_setting.GetLogSetting() = operation_setting.LogSetting{UserExportEnabled: true, MaxRangeDays: 365}
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLogDB
		common.SetLogDatabaseType(oldType)
		*operation_setting.GetLogSetting() = oldSettings
		assert.NoError(t, conn.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.Channel{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: "Fixture channel"}).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: 1, Type: model.LogTypeConsume, CreatedAt: now, ModelName: "chosen-model", TokenName: "chosen-token", ChannelId: 1, Quota: 9007199254740993, RequestId: "selected", Group: "historical", Other: `{"admin_info":{"secret":"private"},"frt":15,"billing_inputs":"private"}`},
		{UserId: 2, Type: model.LogTypeConsume, CreatedAt: now, ModelName: "chosen-model", TokenName: "chosen-token", RequestId: "other-user", Group: "historical"},
		{UserId: 1, Type: model.LogTypeManage, CreatedAt: now, ModelName: "chosen-model", RequestId: "operation"},
		{UserId: 1, Type: model.LogTypeConsume, CreatedAt: now - 366*86400, ModelName: "chosen-model", RequestId: "too-old"},
		{UserId: 1, Type: model.LogTypeConsume, CreatedAt: now, ModelName: "different-model", RequestId: "different"},
	}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("id", 1)
		role, _ := strconv.Atoi(c.GetHeader("X-Test-Role"))
		c.Set("role", role)
		c.Next()
	})
	router.GET("/self/export", ExportUserLogs)
	router.GET("/export", ExportAllLogs)
	router.GET("/self", GetUserLogs)
	router.GET("/self/stat", GetLogsSelfStat)
	router.GET("/data/self", GetUserQuotaDates)
	router.GET("/data/flow/self", GetUserFlowQuotaDates)
	return router
}

func TestLogExportPreservesUserScopeFiltersAndProjection(t *testing.T) {
	router := logExportFixture(t)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/self/export?keyword=chosen&group=historical&username=other&channel=999&user_id=2", nil))
	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "application/x-ndjson; charset=utf-8", response.Header().Get("Content-Type"))
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	lines := strings.Split(response.Body.String(), "\n")
	require.Len(t, lines, 2)
	var row map[string]any
	require.NoError(t, common.Unmarshal([]byte(lines[0]), &row))
	assert.Equal(t, "selected", row["request_id"])
	assert.Equal(t, "9007199254740993", row["quota"])
	assert.Equal(t, "Fixture channel", row["channel_name"])
	for _, field := range []string{"user_id", "username", "channel", "token_id", "ip", "upstream_request_id", "billing_event_id"} {
		assert.NotContains(t, row, field)
	}
	assert.NotContains(t, lines[0], "private")
	assert.JSONEq(t, `{"success":true,"message":"","data":{"count":1}}`, lines[1])
}

func TestLogExportLockDisableAndFailureHaveNoSuccessTerminal(t *testing.T) {
	router := logExportFixture(t)
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		return r
	}
	operation_setting.GetLogSetting().UserExportEnabled = false
	assert.Contains(t, request("/self/export").Body.String(), `"success":false`)
	operation_setting.GetLogSetting().UserExportEnabled = true
	release, ok := service.AcquireLogExportLock(1)
	require.True(t, ok)
	blocked := request("/self/export")
	release()
	release()
	assert.Contains(t, blocked.Body.String(), "Another log export is running")
	assert.Contains(t, request("/self/export?request_id=selected").Body.String(), `"count":1`)
	require.NoError(t, model.LOG_DB.Migrator().DropTable(&model.Log{}))
	failed := request("/self/export")
	assert.Contains(t, failed.Body.String(), `"success":false`)
	assert.NotContains(t, failed.Body.String(), `"success":true`)
}

func TestLogExportAdminScopeAndSharedRangeLimit(t *testing.T) {
	router := logExportFixture(t)
	operation_setting.GetLogSetting().UserExportEnabled = false
	response := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/export?model_name=chosen-model&group=historical", nil)
	req.Header.Set("X-Test-Role", strconv.Itoa(common.RoleAdminUser))
	router.ServeHTTP(response, req)
	assert.Contains(t, response.Body.String(), `"count":2`)
	now := common.GetTimestamp()
	operation_setting.GetLogSetting().MaxRangeDays = 7
	for _, path := range []string{"/self", "/self/stat", "/data/self", "/data/flow/self", "/export"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path+"?start_timestamp="+strconv.FormatInt(now-8*86400, 10)+"&end_timestamp="+strconv.FormatInt(now, 10), nil)
			if path == "/export" {
				req.Header.Set("X-Test-Role", strconv.Itoa(common.RoleAdminUser))
			}
			router.ServeHTTP(r, req)
			assert.Contains(t, r.Body.String(), `"success":false`)
			assert.Contains(t, r.Body.String(), "cannot exceed")
		})
	}
	for _, query := range []string{"start_timestamp=invalid", "start_timestamp=-1", "start_timestamp=9223372036854775807", "start_timestamp=20&end_timestamp=10"} {
		r := httptest.NewRecorder()
		router.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/export?"+query, nil))
		assert.Contains(t, r.Body.String(), `"success":false`)
	}
}

func TestLogExportCancellationReleasesLock(t *testing.T) {
	router := logExportFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/self/export", nil).WithContext(ctx)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	assert.NotContains(t, response.Body.String(), `"success":true`)
	release, ok := service.AcquireLogExportLock(1)
	require.True(t, ok)
	release()
}
