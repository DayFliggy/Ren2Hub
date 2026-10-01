package controller

import (
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const opsUnhealthyRouteLimit = 50

func GetOpsOverview(c *gin.Context) {
	overview, err := service.ReadOpsOverview(c.DefaultQuery("window", "1h"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func GetOpsRequests(c *gin.Context) {
	hours, err := strconv.Atoi(c.DefaultQuery("hours", "24"))
	if err != nil || hours < perfmetrics.OverviewMinHours || hours > perfmetrics.OverviewMaxHours {
		common.ApiErrorMsg(c, fmt.Sprintf("hours must be between %d and %d", perfmetrics.OverviewMinHours, perfmetrics.OverviewMaxHours))
		return
	}
	overview, err := perfmetrics.QueryOverview(hours)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, overview)
}

func GetOpsChannels(c *gin.Context) {
	health, err := model.ReadOpsChannelHealth(opsUnhealthyRouteLimit)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, health)
}

func GetSystemTaskSummary(c *gin.Context) {
	summaries, err := service.SummarizeSystemTasks(time.Now())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"types": summaries})
}
