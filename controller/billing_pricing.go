package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/gin-gonic/gin"
)

func GetBillingPrices(c *gin.Context) {
	prices, err := model.ListActiveBillingPrices(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "billing catalog unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": prices})
}

func PublishBillingPrice(c *gin.Context) {
	var request struct {
		Price           billing.PriceVersion `json:"price" binding:"required"`
		ExpectedVersion int64                `json:"expected_version"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid price"})
		return
	}
	version, err := model.PublishBillingPrice(c.Request.Context(), request.Price, request.ExpectedVersion, c.GetInt("id"))
	if errors.Is(err, model.ErrBillingPriceConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": version})
}

func GetBillingPriceSeed(c *gin.Context) {
	prices, unconfigured, err := model.InitialBillingPriceCatalog()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "initial price catalog unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"prices": prices, "unconfigured": unconfigured, "source": "checked-in-local-defaults"}})
}
