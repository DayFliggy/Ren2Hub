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
	versions, err := model.ListActiveBillingPrices(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	byModel := make(map[string]int, len(versions))
	for i, version := range versions {
		byModel[version.Model] = i
	}
	for _, row := range pricing {
		index, ok := byModel[row.ModelName]
		if !ok {
			continue
		}
		version := versions[index]
		public = append(public, gin.H{"model_name": row.ModelName, "description": row.Description, "icon": row.Icon, "tags": row.Tags,
			"vendor_id": row.VendorID, "owner_by": row.OwnerBy, "supported_endpoint_types": row.SupportedEndpointTypes,
			"channel_ratio_min": row.ChannelRatioMin, "channel_ratio_max": row.ChannelRatioMax, "billing_mode": version.Mode,
			"price_version": version, "billing_expr": version.Expression})
	}
	versionHash, err := billing.Hash(versions)
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
