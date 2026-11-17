package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetHomeRequestMetrics(c *gin.Context) {
	result, err := service.HomeMetrics(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

// GetPricingCount reports how many models GetPricing lists, so the guest home
// page can show the number without downloading the whole catalog.
func GetPricingCount(c *gin.Context) {
	common.ApiSuccess(c, gin.H{"models": len(model.GetPricing())})
}
