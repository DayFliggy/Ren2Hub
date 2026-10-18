package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedemptionListAndSearchSorting(t *testing.T) {
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Redemption{}))
	rows := []model.Redemption{
		{Id: 1, Name: "selected", Key: "sort-1", CreatedTime: 300, RedeemedTime: 10, Status: common.RedemptionCodeStatusUsed},
		{Id: 2, Name: "selected", Key: "sort-2", CreatedTime: 100, RedeemedTime: 30, Status: common.RedemptionCodeStatusUsed},
		{Id: 3, Name: "selected", Key: "sort-3", CreatedTime: 100, RedeemedTime: 20, Status: common.RedemptionCodeStatusUsed},
		{Id: 4, Name: "selected", Key: "sort-4", CreatedTime: 200, RedeemedTime: 20, Status: common.RedemptionCodeStatusUsed},
		{Id: 5, Name: "unselected", Key: "sort-5", CreatedTime: 50, Status: common.RedemptionCodeStatusDisabled},
	}
	require.NoError(t, db.Create(&rows).Error)
	router := gin.New()
	router.GET("/api/redemption/", GetAllRedemptions)
	router.GET("/api/redemption/search", SearchRedemptions)
	for _, tt := range []struct {
		query string
		total int
		ids   []int
	}{
		{query: "/?p=1&page_size=2", total: 5, ids: []int{5, 4}},
		{query: "/?sort_by=created_time&sort_order=asc&p=1&page_size=2", total: 5, ids: []int{5, 2}},
		{query: "/?sort_by=created_time&sort_order=asc&p=2&page_size=2", total: 5, ids: []int{3, 4}},
		{query: "/?sort_by=created_time&sort_order=desc&p=2&page_size=2", total: 5, ids: []int{3, 2}},
		{query: "/?sort_by=used_time&sort_order=asc&p=2&page_size=2", total: 5, ids: []int{3, 4}},
		{query: "/?sort_by=used_time&sort_order=desc&p=1&page_size=2", total: 5, ids: []int{2, 4}},
		{query: "/search?keyword=selected&status=3&sort_by=created_time&sort_order=asc&p=1&page_size=2", total: 4, ids: []int{2, 3}},
		{query: "/search?keyword=selected&status=3&sort_by=used_time&sort_order=desc&p=2&page_size=2", total: 4, ids: []int{3, 1}},
		{query: "/search?status=2&sort_by=id&sort_order=asc", total: 1, ids: []int{5}},
	} {
		t.Run(tt.query, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/redemption"+tt.query, nil))
			require.Equal(t, http.StatusOK, response.Code)
			var result struct {
				Success bool `json:"success"`
				Data    struct {
					Total int                `json:"total"`
					Items []model.Redemption `json:"items"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
			require.True(t, result.Success)
			assert.Equal(t, tt.total, result.Data.Total)
			ids := make([]int, 0, len(result.Data.Items))
			for _, row := range result.Data.Items {
				ids = append(ids, row.Id)
			}
			assert.Equal(t, tt.ids, ids)
		})
	}
	for _, path := range []string{"/api/redemption/", "/api/redemption/search"} {
		for _, query := range []string{"sort_by=quota", "sort_by=id%20desc%3Bdrop%20table%20redemptions", "sort_order=sideways", "sort_order=asc%3Bdrop"} {
			t.Run(path+query, func(t *testing.T) {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?"+query, nil))
				assert.Equal(t, http.StatusBadRequest, response.Code)
			})
		}
	}
	var count int64
	require.NoError(t, db.Model(&model.Redemption{}).Count(&count).Error)
	assert.EqualValues(t, len(rows), count, "invalid sort parameters cannot alter stored rows")
}
