package controller

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.QuotaData{}))
	oldConsume, oldExport, oldErrorLog := common.LogConsumeEnabled, common.DataExportEnabled, constant.ErrorLogEnabled
	common.LogConsumeEnabled, common.DataExportEnabled, constant.ErrorLogEnabled = true, true, true
	settings := operation_setting.GetLogSetting()
	previousSettings := *settings
	settings.MaxRangeDays = 365
	t.Cleanup(func() {
		common.LogConsumeEnabled, common.DataExportEnabled, constant.ErrorLogEnabled = oldConsume, oldExport, oldErrorLog
		*settings = previousSettings
	})
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&[]model.QuotaData{
		{UserID: 1, CreatedAt: today.Unix(), Count: 2, Quota: 12, TokenUsed: 4, ModelName: "fixture"},
		{UserID: 1, CreatedAt: today.Add(-time.Hour).Unix(), Count: 1, Quota: 6, ModelName: "fixture"},
		{UserID: 2, CreatedAt: today.Unix(), Count: 9, Quota: 300, ModelName: "private"},
	}).Error)
	require.NoError(t, db.Create(&[]model.Log{
		{UserId: 1, CreatedAt: today.Unix() + 1, Type: model.LogTypeConsume, UseTime: 2, ModelName: "fixture", RequestId: "settled"},
		{UserId: 1, CreatedAt: today.Unix() + 5, Type: model.LogTypeError, ModelName: "fixture", RequestId: "failed"},
		{UserId: 2, CreatedAt: today.Unix() + 1, Type: model.LogTypeConsume, UseTime: 60, ModelName: "private", RequestId: "other"},
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
	t.Run("counts come from quota_data and personal quality from own logs", func(t *testing.T) {
		success, data, status := call(url.Values{"range": {"today"}})
		require.Equal(t, http.StatusOK, status)
		require.True(t, success)
		require.Equal(t, "quota_data", data.Meta.Source)
		require.EqualValues(t, 12, data.KPI.TotalQuota)
		require.EqualValues(t, 2, data.KPI.TotalRequests)
		require.EqualValues(t, 4, data.KPI.TotalTokens)
		require.NotNil(t, data.Comparison)
		require.Equal(t, 100.0, *data.Comparison.QuotaDelta)
		require.Equal(t, 100.0, *data.Comparison.RequestsDelta)
		require.Len(t, data.Models, 1)
		require.Equal(t, "fixture", data.Models[0].Model)
		require.Equal(t, 2000.0, *data.Models[0].AvgLatency)
		require.Equal(t, 2000.0, *data.KPI.AvgLatency)
		require.Equal(t, 50.0, *data.KPI.SuccessRate)
		require.Len(t, data.Hourly, 24)
		require.Equal(t, today.Unix(), data.Hourly[0].Ts)
		require.EqualValues(t, 2, data.Hourly[0].Requests)
		require.Equal(t, 5, queries)
	})
	t.Run("distribution skips both prior query and comparison", func(t *testing.T) {
		success, data, status := call(url.Values{"range": {"today"}, "include_comparison": {"false"}})
		require.Equal(t, http.StatusOK, status)
		require.True(t, success)
		require.Nil(t, data.Comparison)
		require.EqualValues(t, 12, data.KPI.TotalQuota)
		require.Equal(t, 4, queries)
	})
	t.Run("success rate is unavailable without error logs", func(t *testing.T) {
		constant.ErrorLogEnabled = false
		t.Cleanup(func() { constant.ErrorLogEnabled = true })
		success, data, _ := call(url.Values{"range": {"today"}})
		require.True(t, success)
		require.False(t, data.Meta.Coverage.SuccessRate)
		require.Nil(t, data.KPI.SuccessRate)
		require.NotNil(t, data.KPI.AvgLatency)
		require.Equal(t, 3, queries)
	})
	t.Run("disabled data export reports counts unavailable but keeps own latency", func(t *testing.T) {
		common.DataExportEnabled = false
		t.Cleanup(func() { common.DataExportEnabled = true })
		success, data, _ := call(url.Values{"range": {"today"}})
		require.True(t, success)
		require.False(t, data.Meta.Available)
		require.Zero(t, data.KPI.TotalRequests)
		require.Empty(t, data.Hourly)
		require.Equal(t, 2000.0, *data.KPI.AvgLatency)
		require.Equal(t, 3, queries)
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
	// callMessage returns message_key when the error has one, else message.
	callMessage := func(t *testing.T, params url.Values) (bool, string) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("id", 1)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/data/self/summary?"+params.Encode(), nil)
		GetDashboardSummary(ctx)
		var response struct {
			Success    bool   `json:"success"`
			Message    string `json:"message"`
			MessageKey string `json:"message_key"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		if response.MessageKey != "" {
			return response.Success, response.MessageKey
		}
		return response.Success, response.Message
	}
	// The console mirrors these limits: logged days up to today, plus the
	// whole window again when comparing, must fit max_range_days.
	window := func(first, last time.Time, comparison bool) url.Values {
		return url.Values{"range": {"custom"}, "start": {first.Format("2006-01-02")}, "end": {last.Format("2006-01-02")}, "include_comparison": {strconv.FormatBool(comparison)}}
	}
	day := func(offset int) time.Time { return today.AddDate(0, 0, offset) }
	t.Run("comparison counts toward the log range limit", func(t *testing.T) {
		settings.MaxRangeDays = 30
		cases := []struct {
			name       string
			params     url.Values
			success    bool
			messageKey string
		}{
			{"30 days without comparison", url.Values{"range": {"30d"}, "include_comparison": {"false"}}, true, ""},
			{"30 days with comparison", url.Values{"range": {"30d"}, "include_comparison": {"true"}}, false, "The time range cannot exceed {{days}} days"},
			{"15 days up to today with comparison", window(day(-14), today, true), true, ""},
			{"16 days up to today with comparison", window(day(-15), today, true), false, "The time range cannot exceed {{days}} days"},
			{"15 past days with comparison", window(day(-15), day(-1), true), true, ""},
			{"16 past days with comparison", window(day(-16), day(-1), true), false, "The time range cannot exceed {{days}} days"},
			{"31 days without comparison", window(day(-30), today, false), false, "The time range cannot exceed {{days}} days"},
		}
		for _, tc := range cases {
			success, messageKey := callMessage(t, tc.params)
			require.Equal(t, tc.success, success, tc.name)
			require.Equal(t, tc.messageKey, messageKey, tc.name)
		}
	})
	t.Run("summary keeps its 366 day cap above a larger log limit", func(t *testing.T) {
		settings.MaxRangeDays = 1000
		success, messageKey := callMessage(t, window(day(-365), today, false))
		require.True(t, success)
		require.Empty(t, messageKey)
		success, messageKey = callMessage(t, window(day(-366), today, false))
		require.False(t, success)
		require.Equal(t, "dashboard range must be 1 to 366 days", messageKey)
	})
	t.Run("invalid comparison option is rejected before any database query", func(t *testing.T) {
		success, _, status := call(url.Values{"range": {"today"}, "include_comparison": {"invalid"}})
		require.False(t, success)
		require.Equal(t, http.StatusBadRequest, status)
		require.Zero(t, queries)
	})
}
