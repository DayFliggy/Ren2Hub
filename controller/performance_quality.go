package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/gin-gonic/gin"
)

// maxPerformanceQualityHours 与 perf_metrics 查询窗口的上限一致（30 天）。
const maxPerformanceQualityHours = 24 * 30

// GetPerformanceQuality 返回运维质量卡的跨模型小时序列，权限与 /api/performance/stats 相同。
// 原生 /api/perf-metrics/summary 只有窗口合计和按模型的成功率点，没有跨模型的小时请求量和耗时。
func GetPerformanceQuality(c *gin.Context) {
	hours := 24
	if raw := c.Query("hours"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxPerformanceQualityHours {
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
			return
		}
		hours = parsed
	}
	result, err := perfmetrics.QueryHourlyAll(hours)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
