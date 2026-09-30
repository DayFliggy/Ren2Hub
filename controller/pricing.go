package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func GetPricing(c *gin.Context) {
	pricing, err := service.AvailableRoutePrices(c.Request.Context(), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	versionHash, err := billing.Hash(pricing)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(200, gin.H{
		"success": true, "data": pricing,
		"vendors":            model.GetVendors(),
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"pricing_version":    versionHash,
		"group_ratio":        map[string]float64{}, "usable_group": map[string]string{}, "auto_groups": []string{},
	})
}
