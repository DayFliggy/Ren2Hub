package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

func GetPricing(c *gin.Context) {
	pricing, vendors, endpoints, err := model.ReadPricing()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": pricing, "vendors": vendors, "supported_endpoint": endpoints, "pricing_version": "channel-routing-v1"})
}
