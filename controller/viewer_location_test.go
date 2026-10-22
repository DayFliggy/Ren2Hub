package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestViewerTimezoneEndpoints(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "viewer-tz", AffCode: "VIEWER-TZ"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, TradeNo: "viewer-tz-late", PaymentMethod: "stripe", Currency: "USD", PayAmount: "1", Status: common.TopUpStatusSuccess, CreateTime: time.Date(2026, 10, 20, 23, 30, 0, 0, time.UTC).Unix()}
	require.NoError(t, db.Create(&order).Error)
	queries := 0
	count := func(*gorm.DB) { queries++ }
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:viewer-tz-query", count))
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:viewer-tz-row", count))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("test:viewer-tz-update", count))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id) })
	router.GET("/stats", GetTopUpStatistics)
	router.GET("/affiliate", GetAffiliateInfo)
	router.GET("/dashboard", GetDashboardSummary)
	call := func(path string) (int, map[string]any) {
		queries = 0
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		var body map[string]any
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
		return response.Code, body
	}
	t.Run("stats buckets days in the viewer offset", func(t *testing.T) {
		for path, want := range map[string]string{"/stats": "2026-10-20", "/stats?tz_offset=-480": "2026-10-21"} {
			status, body := call(path)
			require.Equal(t, http.StatusOK, status, path)
			daily := body["data"].(map[string]any)["daily"].([]any)
			require.Len(t, daily, 1, path)
			require.Equal(t, want, daily[0].(map[string]any)["date"], path)
		}
	})
	t.Run("invalid offset is rejected before any query", func(t *testing.T) {
		for _, path := range []string{"/stats?tz_offset=abc", "/affiliate?tz_offset=841", "/dashboard?range=today&tz_offset=-841"} {
			status, body := call(path)
			require.Equal(t, http.StatusBadRequest, status, path)
			require.Equal(t, false, body["success"], path)
			require.Equal(t, "Invalid timezone offset", body["message_key"], path)
			require.Zero(t, queries, path)
		}
	})
}
