package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 前端用 today 标注“今天”，它必须与 checked_in_today 的判定使用同一个服务器日期。
func TestCheckinStatusReportsTheServerDateUsedForTodayCheck(t *testing.T) {
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	previousDB := model.DB
	model.DB = db
	setting := operation_setting.GetCheckinSetting()
	previousSetting := *setting
	setting.Enabled = true
	t.Cleanup(func() {
		model.DB = previousDB
		*setting = previousSetting
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.Checkin{}))
	today := time.Now().Format("2006-01-02")
	require.NoError(t, db.Create(&model.Checkin{UserId: 7, CheckinDate: today, QuotaAwarded: 1000, CreatedAt: time.Now().Unix()}).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/user/checkin", nil)
	c.Set("id", 7)
	GetCheckinStatus(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Today string `json:"today"`
			Stats struct {
				CheckedInToday bool `json:"checked_in_today"`
			} `json:"stats"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.True(t, body.Success)
	require.Equal(t, today, body.Data.Today)
	require.True(t, body.Data.Stats.CheckedInToday)
}
