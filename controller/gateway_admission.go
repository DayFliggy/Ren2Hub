package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type gatewayEffectiveAdmission struct {
	Concurrency      int `json:"concurrency"`
	WaitMS           int `json:"wait_ms"`
	QueueSize        int `json:"queue_size"`
	ChannelQueueSize int `json:"channel_queue_size"`
}

func GetGatewayMetrics(c *gin.Context) {
	metrics, err := service.ReadGatewayMetrics(c.Request.Context())
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, metrics)
}

func GetGatewayConfig(c *gin.Context) {
	config, err := model.GetGatewayConfig()
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, config)
}

func UpdateGatewayConfig(c *gin.Context) {
	var config model.GatewayConfig
	if err := c.ShouldBindJSON(&config); err != nil || config.Version < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid gateway configuration", "code": "VALIDATION_ERROR"})
		return
	}
	if err := config.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}
	if err := model.SaveGatewayConfig(config); err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	config.Version++
	recordManageAuditFor(c, 0, "gateway.config.update", map[string]interface{}{"version": config.Version})
	common.ApiSuccess(c, config)
}

func GetGatewayUserAdmission(c *gin.Context) {
	user, ok := gatewayAdmissionUser(c)
	if !ok {
		return
	}
	config, err := model.GetGatewayConfig()
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	policy, err := service.ResolveGatewayAdmissionPolicy(config, user)
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"user_id": user.Id, "group": user.Group, "config_version": config.Version,
		"override":  model.GatewayAdmissionOverride{Concurrency: user.GatewayConcurrency, WaitMS: user.GatewayWaitMS, QueueSize: user.GatewayQueueSize},
		"effective": gatewayEffectiveAdmission{Concurrency: policy.UserConcurrency, WaitMS: int(policy.Wait.Milliseconds()), QueueSize: policy.UserQueueSize, ChannelQueueSize: policy.ChannelQueueSize},
		"sources":   policy.Sources,
	})
}

func UpdateGatewayUserAdmission(c *gin.Context) {
	user, ok := gatewayAdmissionUser(c)
	if !ok {
		return
	}
	var override model.GatewayAdmissionOverride
	if err := c.ShouldBindJSON(&override); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid gateway admission override", "code": "VALIDATION_ERROR"})
		return
	}
	if err := override.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}
	query := model.DB.WithContext(c.Request.Context()).Model(&model.User{}).Where("id = ?", user.Id)
	if c.GetInt("role") < common.RoleRootUser {
		query = query.Where("role < ?", c.GetInt("role"))
	}
	result := query.Updates(map[string]interface{}{
		"gateway_concurrency": override.Concurrency, "gateway_wait_ms": override.WaitMS, "gateway_queue_size": override.QueueSize,
	})
	if result.Error != nil {
		writeGatewayAPIError(c, result.Error)
		return
	}
	if result.RowsAffected == 0 {
		// MySQL reports zero changed rows for an idempotent update.
		var matched int64
		if err := query.Count(&matched).Error; err != nil {
			writeGatewayAPIError(c, err)
			return
		}
		if matched != 1 {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": "gateway user changed during update", "code": "CONFLICT"})
			return
		}
	}
	recordManageAuditFor(c, user.Id, "gateway.user.update", map[string]interface{}{"concurrency": override.Concurrency, "wait_ms": override.WaitMS, "queue_size": override.QueueSize})
	common.ApiSuccess(c, override)
}

func GetGatewayUserRuntime(c *gin.Context) {
	user, ok := gatewayAdmissionUser(c)
	if !ok {
		return
	}
	if !common.RedisEnabled {
		writeGatewayAPIError(c, service.ErrRouteLeaseUnavailable)
		return
	}
	runtime, err := service.ReadGatewayResourceRuntime(c.Request.Context(), common.RDB, service.UserRouteLeaseKey(user.Id))
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, runtime)
}

func GetGatewayChannelRuntime(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id", "code": "VALIDATION_ERROR"})
		return
	}
	var channel model.Channel
	if err := model.DB.WithContext(c.Request.Context()).Select("id", "capacity_total").First(&channel, channelID).Error; err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	capacity, _, err := channel.RoutingSettings()
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	if !common.RedisEnabled {
		writeGatewayAPIError(c, service.ErrRouteLeaseUnavailable)
		return
	}
	runtime, err := service.ReadGatewayResourceRuntime(c.Request.Context(), common.RDB, service.ChannelRouteLeaseKey(channelID))
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	rate, err := service.ReadGatewayProviderRateRuntime(c.Request.Context(), common.RDB, service.GatewayChannelRateScopeID(channelID))
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"channel_id": channelID, "capacity": capacity, "in_flight": runtime.InFlight, "queued": runtime.Queued, "rate": rate})
}

func ListGatewayChannelCredentials(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel id", "code": "VALIDATION_ERROR"})
		return
	}
	var channel model.Channel
	if err := model.DB.WithContext(c.Request.Context()).Select("id").First(&channel, channelID).Error; err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	credentials := []model.GatewayCredential{}
	if err := model.DB.WithContext(c.Request.Context()).Select("id").Where("channel_id = ?", channelID).Order("created_at, id").Find(&credentials).Error; err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, credentials)
}

func GetGatewayCredentialRuntime(c *gin.Context) {
	var credential model.GatewayCredential
	if err := model.DB.WithContext(c.Request.Context()).Select("id").Where("id = ?", c.Param("id")).First(&credential).Error; err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	if !common.RedisEnabled {
		writeGatewayAPIError(c, service.ErrRouteLeaseUnavailable)
		return
	}
	runtime, err := service.ReadGatewayProviderRateRuntime(c.Request.Context(), common.RDB, service.GatewayCredentialRateScopeID(credential.ID))
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, runtime)
}

func gatewayAdmissionUser(c *gin.Context) (model.User, bool) {
	userID := c.GetInt("id")
	if c.Param("id") != "" {
		parsed, err := strconv.Atoi(c.Param("id"))
		if err != nil || parsed <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user id", "code": "VALIDATION_ERROR"})
			return model.User{}, false
		}
		userID = parsed
	}
	var user model.User
	if err := model.DB.WithContext(c.Request.Context()).Select("id", "role", "group", "gateway_concurrency", "gateway_wait_ms", "gateway_queue_size").First(&user, userID).Error; err != nil {
		writeGatewayAPIError(c, err)
		return user, false
	}
	if c.Param("id") != "" && c.GetInt("role") < common.RoleRootUser && user.Role >= c.GetInt("role") {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "insufficient permission", "code": "FORBIDDEN"})
		return user, false
	}
	return user, true
}

func writeGatewayAPIError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrGatewayConfigReference) || errors.Is(err, model.ErrGatewayTPMUnsupported) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error(), "code": "VALIDATION_ERROR"})
		return
	}
	if errors.Is(err, model.ErrGatewayConfigConflict) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error(), "code": "CONFLICT"})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "gateway resource not found", "code": "NOT_FOUND"})
		return
	}
	common.SysError("gateway administration failed: " + err.Error())
	c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "gateway runtime unavailable", "code": "GATEWAY_UNAVAILABLE"})
}
