package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetAutomaticBillingPricing(c *gin.Context) {
	status, err := service.GetAutomaticPricingStatus(c.Request.Context())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

func ListAutomaticBillingPricingSnapshots(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	rows, err := model.ListBillingPricingSnapshots(c.Request.Context(), c.Query("source"), limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func GetAutomaticBillingPricingSnapshot(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid pricing snapshot id"})
		return
	}
	snapshot, err := model.GetBillingPricingSnapshot(c.Request.Context(), id)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, gorm.ErrRecordNotFound) {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", "attachment; filename=pricing-"+snapshot.Source+"-"+snapshot.Hash+".json")
		c.Header("X-Pricing-SHA256", snapshot.Hash)
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(snapshot.Document))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": struct {
		*model.BillingPricingSnapshot
		Document string `json:"document"`
	}{snapshot, string(snapshot.Document)}})
}

func SaveAutomaticBillingPricing(c *gin.Context) {
	var input struct {
		Enabled              *bool `json:"enabled"`
		CheckIntervalMinutes int   `json:"check_interval_minutes"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "enabled and check_interval_minutes are required"})
		return
	}
	config := model.BillingPricingConfig{Enabled: *input.Enabled, CheckIntervalMinutes: input.CheckIntervalMinutes}
	if err := model.SaveBillingPricingConfig(c.Request.Context(), &config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": config})
}

func SyncAutomaticBillingPricing(c *gin.Context) {
	task, _, err := service.EnqueueSystemTask(model.SystemTaskTypeBillingPricingSync, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": task.ToResponse()})
}

func RestoreAutomaticBillingPricing(c *gin.Context) {
	var input struct {
		Model           string `json:"model" binding:"required"`
		ExpectedVersion *int64 `json:"expected_version"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.ShouldBindJSON(&input); err != nil || input.ExpectedVersion == nil || *input.ExpectedVersion < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "model and expected_version are required"})
		return
	}
	price, err := service.RestoreAutomaticPrice(c.Request.Context(), input.Model, *input.ExpectedVersion)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, model.ErrBillingPriceConflict) || errors.Is(err, model.ErrBillingPricingSourceConflict) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": price})
}

type billingPricingSyncHandler struct{}

func (billingPricingSyncHandler) Type() string    { return model.SystemTaskTypeBillingPricingSync }
func (billingPricingSyncHandler) NewPayload() any { return nil }
func (billingPricingSyncHandler) Enabled() bool {
	config, err := model.GetBillingPricingConfig(context.Background())
	if err != nil {
		common.SysError("read automatic pricing configuration: " + err.Error())
		return false
	}
	return config.Enabled
}
func (billingPricingSyncHandler) Interval() time.Duration {
	config, err := model.GetBillingPricingConfig(context.Background())
	if err != nil {
		return 30 * time.Minute
	}
	return time.Duration(config.CheckIntervalMinutes) * time.Minute
}
func (billingPricingSyncHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	result, err := service.SyncAutomaticPricing(ctx)
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, result, err)
}
