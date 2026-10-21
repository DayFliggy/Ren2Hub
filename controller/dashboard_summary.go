package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetDashboardSummary(c *gin.Context) {
	includeComparison := true
	if value := c.Query("include_comparison"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("invalid include_comparison"))
			return
		}
		includeComparison = parsed
	}
	offset := 0
	if value := c.Query("tz_offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("invalid tz_offset"))
			return
		}
		offset = parsed
	}
	start, end, err := service.ParseDashboardRange(c.Query("range"), c.Query("start"), c.Query("end"), offset, time.Now())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}
	// Every queried interval stays inside the same retention/permission boundary.
	queryStart := start.Unix()
	if includeComparison {
		queryStart = start.Add(-end.Sub(start)).Unix()
	}
	bounded, err := limitLogTimeRange(queryStart, end.Unix()-1)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if bounded != queryStart {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("dashboard query exceeds the allowed log range"))
		return
	}
	result, err := service.Dashboard(c.Request.Context(), c.GetInt("id"), start, end, offset, includeComparison)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetRoutingPerformance(c *gin.Context) { common.ApiSuccess(c, service.RoutingSnapshot()) }
