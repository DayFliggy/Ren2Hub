package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 普通用户的 start_time 不能越过 30 天截止点；管理员列表可以查更早的订单。
func TestUserTopUpListCannotBypassQueryWindow(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "topup-window", AffCode: "TOPUP-WINDOW"}
	require.NoError(t, db.Create(&user).Error)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&model.TopUp{UserId: user.Id, TradeNo: "window-recent", CreateTime: now}).Error)
	require.NoError(t, db.Create(&model.TopUp{UserId: user.Id, TradeNo: "window-old", CreateTime: now - 40*24*3600}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id) })
	router.GET("/self", GetUserTopUps)
	router.GET("/all", GetAllTopUps)
	start := strconv.FormatInt(now-50*24*3600, 10)
	for path, want := range map[string]int{"/self?start_time=" + start: 1, "/all?start_time=" + start: 2} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
		require.True(t, result.Success, response.Body.String())
		assert.Equal(t, want, result.Data.Total, path)
	}
}
