package controller

import (
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	c.JSON(200, gin.H{"success": true, "data": pricing, "vendors": model.GetVendors(), "supported_endpoint": model.GetSupportedEndpointMap(), "pricing_version": "channel-routing-v1"})
}
