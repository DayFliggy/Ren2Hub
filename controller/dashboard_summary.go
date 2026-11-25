package controller

import (
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetDashboardSummary(c *gin.Context) {
	getDashboardSummary(c, model.DashboardUserScope(c.GetInt("id")))
}

func GetSiteDashboardSummary(c *gin.Context) {
	getDashboardSummary(c, model.DashboardSiteScope())
}

func getDashboardSummary(c *gin.Context, scope model.DashboardScope) {
	includeComparison := true
	if value := c.Query("include_comparison"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("invalid include_comparison"))
			return
		}
		includeComparison = parsed
	}
	loc, ok := viewerLocation(c)
	if !ok {
		return
	}
	start, end, err := service.ParseDashboardRange(c.Query("range"), c.Query("start"), c.Query("end"), loc, time.Now())
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return
	}
	// Every queried interval stays inside the same retention/permission boundary.
	queryStart := start.Unix()
	if includeComparison {
		queryStart = start.Add(-end.Sub(start)).Unix()
	}
	// Only the error matters: a nonzero start comes back unchanged.
	if _, err := limitLogTimeRange(queryStart, end.Unix()-1); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := service.Dashboard(c.Request.Context(), scope, start, end, includeComparison)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// viewerLocation reads tz_offset for endpoints that bucket by the viewer's
// calendar; it answers 400 itself when the value is invalid.
func viewerLocation(c *gin.Context) (*time.Location, bool) {
	loc, err := service.ParseViewerLocation(c.Query("tz_offset"))
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
		return nil, false
	}
	return loc, true
}

func GetRoutingPerformance(c *gin.Context) {
	snap := service.RoutingSnapshot()
	service.EnrichRoutingSnapshotNames(c.Request.Context(), &snap)
	common.ApiSuccess(c, snap)
}
