package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDashboardSummaryComparisonWindow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	oldConsume := common.LogConsumeEnabled
	common.LogConsumeEnabled = true
	settings := operation_setting.GetLogSetting()
	previousSettings := *settings
	settings.MaxRangeDays = 365
	t.Cleanup(func() { common.LogConsumeEnabled = oldConsume; *settings = previousSettings })
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: 1, CreatedAt: today.Unix() + 1, Type: model.LogTypeConsume, Quota: 12, PromptTokens: 4, ModelName: "fixture"},
		{UserId: 1, CreatedAt: today.Add(-time.Hour).Unix(), Type: model.LogTypeConsume, Quota: 6, ModelName: "fixture"},
		{UserId: 2, CreatedAt: today.Unix() + 1, Type: model.LogTypeConsume, Quota: 300, ModelName: "private"},
	}).Error)
	queries := 0
	require.NoError(t, db.Callback().Row().After("gorm:row").Register("test:dashboard-query-count", func(*gorm.DB) { queries++ }))
	call := func(params url.Values) (bool, service.DashboardSummary, int) {
		queries = 0
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 1)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self/summary?"+params.Encode(), nil)
		GetDashboardSummary(ctx)
		var response struct {
			Success bool                     `json:"success"`
			Data    service.DashboardSummary `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return response.Success, response.Data, recorder.Code
	}
	t.Run("default includes prior period and isolates user", func(t *testing.T) {
		success, data, status := call(url.Values{"range": {"today"}})
		require.Equal(t, http.StatusOK, status)
		require.True(t, success)
		require.EqualValues(t, 12, data.KPI.TotalQuota)
		require.NotNil(t, data.Comparison)
		require.Equal(t, 100.0, *data.Comparison.QuotaDelta)
		require.Len(t, data.Models, 1)
		require.Equal(t, "fixture", data.Models[0].Model)
		require.Equal(t, 3, queries)
	})
	t.Run("distribution skips both prior query and comparison", func(t *testing.T) {
		success, data, status := call(url.Values{"range": {"today"}, "include_comparison": {"false"}})
		require.Equal(t, http.StatusOK, status)
		require.True(t, success)
		require.Nil(t, data.Comparison)
		require.EqualValues(t, 12, data.KPI.TotalQuota)
		require.Equal(t, 2, queries)
	})
	t.Run("long current window does not need twice its retention", func(t *testing.T) {
		params := url.Values{"range": {"custom"}, "start": {today.AddDate(0, 0, -200).Format("2006-01-02")}, "end": {today.Format("2006-01-02")}}
		success, _, _ := call(params)
		require.False(t, success)
		require.Zero(t, queries)
		params.Set("include_comparison", "false")
		success, data, status := call(params)
		require.Equal(t, http.StatusOK, status)
		require.True(t, success)
		require.Nil(t, data.Comparison)
		require.EqualValues(t, 18, data.KPI.TotalQuota)
	})
	t.Run("comparison opt-out cannot bypass the authorized range", func(t *testing.T) {
		settings.MaxRangeDays = 30
		success, _, _ := call(url.Values{"range": {"custom"}, "start": {today.AddDate(0, 0, -31).Format("2006-01-02")}, "end": {today.Format("2006-01-02")}, "include_comparison": {"false"}})
		require.False(t, success)
		require.Zero(t, queries)
	})
	t.Run("invalid comparison option is rejected before any database query", func(t *testing.T) {
		success, _, status := call(url.Values{"range": {"today"}, "include_comparison": {"invalid"}})
		require.False(t, success)
		require.Equal(t, http.StatusBadRequest, status)
		require.Zero(t, queries)
	})
}
