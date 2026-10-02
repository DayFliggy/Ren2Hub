package controller

import (
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

func providerUsageChannelID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id"})
		return 0, false
	}
	return id, true
}

func respondProviderUsage(c *gin.Context, view service.ProviderUsageView, err error) {
	status, message := http.StatusOK, ""
	switch {
	case errors.Is(err, model.ErrProviderUsageCooldown):
		status, message = http.StatusTooManyRequests, "provider usage refresh is in cooldown"
		seconds := view.NextRefreshAt - time.Now().Unix()
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", strconv.FormatInt(seconds, 10))
	case errors.Is(err, model.ErrProviderUsageIdentityChanged):
		status, message = http.StatusConflict, "channel identity changed; reload and retry"
	case errors.Is(err, service.ErrProviderUsageUnsupported):
		status, message = http.StatusBadRequest, "provider usage requires a single-credential Codex channel"
	case errors.Is(err, gorm.ErrRecordNotFound):
		status, message = http.StatusNotFound, "channel not found"
	case err != nil:
		common.SysError("provider usage operation failed: " + common.MaskSensitiveInfo(err.Error()))
		status, message = http.StatusInternalServerError, "provider usage operation failed"
	}
	c.JSON(status, gin.H{"success": err == nil, "message": message, "data": view})
}

// The router protects reads with ChannelRead and refresh/settings with ChannelOperate.
func GetChannelProviderUsage(c *gin.Context) {
	id, ok := providerUsageChannelID(c)
	if !ok {
		return
	}
	view, err := service.GetProviderUsage(c.Request.Context(), id)
	respondProviderUsage(c, view, err)
}

func RefreshChannelProviderUsage(c *gin.Context) {
	id, ok := providerUsageChannelID(c)
	if !ok {
		return
	}
	view, err := service.RefreshProviderUsage(c.Request.Context(), id)
	respondProviderUsage(c, view, err)
}

func UpdateChannelProviderUsageSettings(c *gin.Context) {
	id, ok := providerUsageChannelID(c)
	if !ok {
		return
	}
	var input struct {
		AutoRefresh *bool `json:"auto_refresh"`
	}
	if err := common.DecodeJson(c.Request.Body, &input); err != nil || input.AutoRefresh == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "auto_refresh must be a boolean"})
		return
	}
	view, err := service.SetProviderUsageAutoRefresh(c.Request.Context(), id, *input.AutoRefresh)
	respondProviderUsage(c, view, err)
}
