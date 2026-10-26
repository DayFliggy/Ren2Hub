package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetDashboardSystemStatus serves the console system-status card to every
// signed-in user from the cached instance observation.
func GetDashboardSystemStatus(c *gin.Context) {
	status, err := service.ReadDashboardSystemStatus(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, status)
}
