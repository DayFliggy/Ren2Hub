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
	public := make([]gin.H, 0, len(pricing))
	for _, row := range pricing {
		public = append(public, gin.H{"model_name": row.ModelName, "description": row.Description, "icon": row.Icon, "tags": row.Tags,
			"vendor_id": row.VendorID, "owner_by": row.OwnerBy, "supported_endpoint_types": row.SupportedEndpointTypes,
			"billing_mode": row.BillingMode, "price_version": row.PriceVersion, "price_versions": row.PriceVersions, "billing_expr": row.BillingExpr})
	}
	versionHash, err := billing.Hash(public)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(200, gin.H{
		"success": true, "data": public,
		"vendors":            model.GetVendors(),
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"pricing_version":    versionHash,
	})
}
