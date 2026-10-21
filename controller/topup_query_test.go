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

// 排行接口的 JSON 字段是 Vue 订单页的合同：items[].{user_id,username,paid_count,credited_quota}。
func TestTopUpRankingResponseContract(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "ranking-contract", AffCode: "RANKING-CONTRACT"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.TopUp{UserId: user.Id, TradeNo: "ranking-contract-order", Status: common.TopUpStatusSuccess, CreditedQuota: 42, CreateTime: common.GetTimestamp()}).Error)
	router := gin.New()
	router.GET("/ranking", GetTopUpRanking)
	invoke := func(path string) map[string]json.RawMessage {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		var result map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
		return result
	}
	result := invoke("/ranking")
	require.JSONEq(t, "true", string(result["success"]))
	var data struct {
		Items     []map[string]any `json:"items"`
		StartTime int64            `json:"start_time"`
	}
	require.NoError(t, json.Unmarshal(result["data"], &data))
	require.Len(t, data.Items, 1)
	assert.Equal(t, map[string]any{"user_id": float64(user.Id), "username": "ranking-contract", "paid_count": float64(1), "credited_quota": float64(42)}, data.Items[0])
	assert.Positive(t, data.StartTime)
	for _, limit := range []string{"abc", "0", "51"} {
		assert.JSONEq(t, "false", string(invoke("/ranking?limit=" + limit)["success"]), limit)
	}
}
