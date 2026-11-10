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

// 不传 start_time 时只有本人查询套用 30 天截止点；管理员列表、搜索、统计和排行
// 都看全部历史，带 user_id 的管理员查询也不算本人查询。
func TestAdminTopUpQueriesHaveNoDefaultWindow(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "topup-default", AffCode: "TOPUP-DEFAULT"}
	other := model.User{Username: "topup-default-other", AffCode: "TOPUP-DEFAULT-OTHER"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&other).Error)
	now := common.GetTimestamp()
	for _, row := range []model.TopUp{
		{UserId: user.Id, TradeNo: "default-recent", Status: common.TopUpStatusSuccess, CreditedQuota: 10, CreateTime: now - 24*3600},
		{UserId: user.Id, TradeNo: "default-old", Status: common.TopUpStatusSuccess, CreditedQuota: 20, CreateTime: now - 40*24*3600},
		{UserId: other.Id, TradeNo: "default-other", Status: common.TopUpStatusSuccess, CreditedQuota: 5, CreateTime: now - 40*24*3600},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id) })
	router.GET("/self", GetUserTopUps)
	router.GET("/all", GetAllTopUps)
	router.GET("/stats", GetTopUpStatistics)
	router.GET("/ranking", GetTopUpRanking)
	invoke := func(path string, data any) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		result := struct {
			Success bool `json:"success"`
			Data    any  `json:"data"`
		}{Data: data}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result), response.Body.String())
		require.True(t, result.Success, path+": "+response.Body.String())
	}
	userID := strconv.Itoa(user.Id)
	for path, want := range map[string]int{
		"/self":                     1,
		"/self?keyword=default-old": 0,
		"/all":                      3,
		"/all?keyword=default-old":  1,
		"/all?user_id=" + userID:    2,
		"/all?user_id=" + userID + "&p=1&page_size=100": 2,
	} {
		var page struct {
			Total int `json:"total"`
		}
		invoke(path, &page)
		assert.Equal(t, want, page.Total, path)
	}
	var stats struct {
		Total     int   `json:"total"`
		StartTime int64 `json:"start_time"`
	}
	invoke("/stats?user_id="+userID, &stats)
	assert.Equal(t, 2, stats.Total)
	assert.Zero(t, stats.StartTime)
	var ranking struct {
		Items []struct {
			UserID        int   `json:"user_id"`
			CreditedQuota int64 `json:"credited_quota"`
		} `json:"items"`
	}
	invoke("/ranking", &ranking)
	require.Len(t, ranking.Items, 2)
	assert.Equal(t, user.Id, ranking.Items[0].UserID)
	assert.EqualValues(t, 30, ranking.Items[0].CreditedQuota, "the 40-day-old order is ranked")
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
	assert.Zero(t, data.StartTime, "admin ranking has no default lower bound")
	for _, limit := range []string{"abc", "0", "51"} {
		assert.JSONEq(t, "false", string(invoke("/ranking?limit=" + limit)["success"]), limit)
	}
}

// Only the administrator detail carries the frozen discount decision; the
// owner's detail and list never include shadow rates or reasons.
func TestTopUpDiscountDecisionIsAdminOnly(t *testing.T) {
	db := useGmpayControllerDB(t)
	user := model.User{Username: "discount-owner", AffCode: "DISCOUNT-OWNER"}
	require.NoError(t, db.Create(&user).Error)
	order := &model.TopUp{UserId: user.Id, TradeNo: "discount-detail", PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusPending,
		Amount: 1, ExpectedQuota: 500000, Currency: "CNY", PayAmount: "6.94", CreateTime: common.GetTimestamp(),
		Discount: &model.TopUpDiscount{ConfigVersion: 3, ListPayAmount: "7.3", Decision: `{"shadow_rates":{"month":"0.05"}}`}}
	require.NoError(t, order.Insert())
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id) })
	router.GET("/detail/:trade_no", GetTopUpDetail)
	router.GET("/admin/detail/:trade_no", GetAdminTopUpDetail)
	router.GET("/self", GetUserTopUps)
	for path, admin := range map[string]bool{"/detail/discount-detail": false, "/self": false, "/admin/detail/discount-detail": true} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Contains(t, response.Body.String(), `"success":true`, path)
		if admin {
			assert.Contains(t, response.Body.String(), `"shadow_rates`, path)
			assert.Contains(t, response.Body.String(), `"config_version":3`, path)
		} else {
			assert.NotContains(t, response.Body.String(), "shadow", path)
			assert.NotContains(t, response.Body.String(), `"discount"`, path)
		}
	}
}
