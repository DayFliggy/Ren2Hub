package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type channelRoutePolicyRequest struct {
	ID                    int    `json:"id"`
	CanonicalModel        string `json:"canonical_model"`
	MaxUserConcurrency    int    `json:"max_user_concurrency"`
	MaxTokenConcurrency   int    `json:"max_token_concurrency"`
	MaxChannelConcurrency int    `json:"max_channel_concurrency"`
	Enabled               bool   `json:"enabled"`
	Version               int64  `json:"version"`
}

func GetChannelRoutePolicy(c *gin.Context) {
	channelID, modelName, ok := parseChannelRoutePolicyParams(c)
	if !ok {
		return
	}
	policy, err := model.FindChannelRoutePolicy(c, channelID, modelName)
	if errors.Is(err, model.ErrChannelRoutePolicyNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "NOT_FOUND"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "INTERNAL_ERROR"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": policy})
}

func UpdateChannelRoutePolicy(c *gin.Context) {
	channelID, _, ok := parseChannelRoutePolicyParams(c)
	if !ok {
		return
	}
	var input channelRoutePolicyRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "BAD_REQUEST"})
		return
	}
	canonical := modellab.NormalizeModel(input.CanonicalModel)
	if canonical == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "BAD_REQUEST"})
		return
	}
	policy, err := service.SaveChannelRoutePolicy(model.ChannelRoutePolicy{
		ID: input.ID, ChannelID: channelID, CanonicalModel: canonical,
		MaxUserConcurrency: input.MaxUserConcurrency, MaxTokenConcurrency: input.MaxTokenConcurrency,
		MaxChannelConcurrency: input.MaxChannelConcurrency, Enabled: input.Enabled, Version: input.Version,
	})
	if errors.Is(err, service.ErrRoutePolicyConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "code": "VERSION_CONFLICT"})
		return
	}
	if errors.Is(err, service.ErrRoutePolicyInvalid) {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"success": false, "code": "VALIDATION_ERROR"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "INTERNAL_ERROR"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": policy})
}

func parseChannelRoutePolicyParams(c *gin.Context) (int, string, bool) {
	channelID, err := strconv.Atoi(c.Param("id"))
	modelName := modellab.NormalizeModel(strings.TrimSpace(c.Query("model")))
	if err != nil || channelID <= 0 || modelName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "BAD_REQUEST"})
		return 0, "", false
	}
	if model.DB == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "INTERNAL_ERROR"})
		return 0, "", false
	}
	var channel model.Channel
	if err := model.DB.Select("id").Where("id = ?", channelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "code": "NOT_FOUND"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "code": "INTERNAL_ERROR"})
		}
		return 0, "", false
	}
	return channelID, modelName, true
}
