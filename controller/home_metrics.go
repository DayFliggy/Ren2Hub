package controller

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// GetHomeRequestMetrics exposes aggregate-only request telemetry for the
// public landing page. No identity or model-level data leaves this endpoint.
func GetHomeRequestMetrics(c *gin.Context) {
	metrics, err := model.GetHomeRequestMetricsWithContext(c.Request.Context(), time.Now())
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		logger.LogError(c.Request.Context(), common.LogText("home request metrics query failed: %v", err))
		common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("home request metrics unavailable"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    metrics,
	})
}
