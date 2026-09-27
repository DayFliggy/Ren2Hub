package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

// PreviewBillingPrice evaluates a draft without publishing or reserving funds.
// RootAuth and DisableCache are enforced by the billing router group.
func PreviewBillingPrice(c *gin.Context) {
	var request struct {
		Price        billing.PriceVersion   `json:"price" binding:"required"`
		Usage        billing.Usage          `json:"usage" binding:"required"`
		Attributes   map[string]interface{} `json:"attributes"`
		RequestFacts map[string]interface{} `json:"request_facts"`
		ChannelRatio string                 `json:"channel_ratio" binding:"required"`
		FrozenAt     time.Time              `json:"frozen_at" binding:"required"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256*1024)
	if err := common.DecodeJsonNumbers(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid price preview"})
		return
	}
	if err := binding.Validator.ValidateStruct(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid price preview"})
		return
	}
	quote, err := billing.NewQuoteWithFacts(request.Price, request.ChannelRatio, request.Attributes, request.FrozenAt, request.RequestFacts)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	charge, err := billing.Calculate(quote, request.Usage)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"quote": quote, "charge": charge}})
}

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
		PluginKey       string               `json:"plugin_key"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid price"})
		return
	}
	price, err := service.ValidateTaskPluginPricePublication(request.Price, request.PluginKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	version, err := model.PublishBillingPrice(c.Request.Context(), price, request.ExpectedVersion, c.GetInt("id"))
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

func GetTaskPluginBillingSchema(c *gin.Context) {
	schema, err := service.ResolveTaskPluginPriceSchema(c.Query("model"), c.Query("plugin_key"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": schema})
}

func GetBillingPriceSeed(c *gin.Context) {
	prices, unconfigured, source, err := service.AutomaticPricingDrafts(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "initial price catalog unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"prices": prices, "unconfigured": unconfigured, "source": source}})
}
