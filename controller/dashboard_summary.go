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
	// Include the comparison interval in the same retention/permission boundary.
	previous := start.Add(-end.Sub(start)).Unix()
	bounded, err := limitLogTimeRange(previous, end.Unix()-1)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if bounded != previous {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("dashboard range including comparison exceeds the allowed log range"))
		return
	}
	result, err := service.Dashboard(c.Request.Context(), c.GetInt("id"), start, end, offset)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func GetRoutingPerformance(c *gin.Context) { common.ApiSuccess(c, service.RoutingSnapshot()) }
