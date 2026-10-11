package controller

import (
	"bytes"
	"encoding/json"
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
	config, err := model.GetGatewayConfigContext(c.Request.Context())
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	common.ApiSuccess(c, config)
}

func UpdateGatewayConfig(c *gin.Context) {
	config, err := model.GetGatewayConfigContext(c.Request.Context())
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "VALIDATION_ERROR"})
		return
	}
	for _, key := range []string{"total_queue_size", "retry_budget_enabled", "retry_budget_percent", "retry_budget_burst", "max_channel_ratio"} {
		if value, exists := fields[key]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "VALIDATION_ERROR"})
			return
		}
	}
	// Only the new fields inherit on omission; the optimistic version is mandatory.
	// max_channel_ratio must be carried over explicitly, otherwise an unrelated
	// configuration update would silently disable the price ceiling.
	config = model.GatewayConfig{TotalQueueSize: config.TotalQueueSize, RetryBudgetEnabled: config.RetryBudgetEnabled, RetryBudgetPercent: config.RetryBudgetPercent, RetryBudgetBurst: config.RetryBudgetBurst, MaxChannelRatio: config.MaxChannelRatio}
	encoded, encodeErr := common.Marshal(fields)
	if encodeErr != nil {
		writeGatewayAPIError(c, encodeErr)
		return
	}
	if err := common.DecodeJson(bytes.NewReader(encoded), &config); err != nil || config.Version < 1 {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid gateway configuration"), gin.H{"code": "VALIDATION_ERROR"})
		return
	}
	if err := config.Validate(); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err, gin.H{"code": "VALIDATION_ERROR"})
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
	config, err := model.GetGatewayConfigContext(c.Request.Context())
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
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid gateway admission override"), gin.H{"code": "VALIDATION_ERROR"})
		return
	}
	if err := override.Validate(); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, err, gin.H{"code": "VALIDATION_ERROR"})
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
			common.ApiErrorStatus(c, http.StatusConflict, common.NewMessage("The user changed during the gateway admission update"), gin.H{"code": "CONFLICT"})
			return
		}
	}
	model.NotifyGatewayAdmissionChanged("user", user.Id)
	recordManageAuditFor(c, user.Id, "gateway.user.update", map[string]interface{}{"concurrency": override.Concurrency, "wait_ms": override.WaitMS, "queue_size": override.QueueSize})
	common.ApiSuccess(c, override)
}

func GetGatewayUserRuntime(c *gin.Context) {
	user, ok := gatewayAdmissionUser(c)
	if !ok {
		return
	}
	config, err := model.GetGatewayConfigContext(c.Request.Context())
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	policy, err := service.ResolveGatewayAdmissionPolicy(config, user)
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	runtime := service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(user.Id))
	runtime.Capacity, runtime.ConfigVersion = policy.UserConcurrency, config.Version
	common.ApiSuccess(c, runtime)
}

func GetGatewayChannelRuntime(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "code": "VALIDATION_ERROR"})
		return
	}
	var channel model.Channel
	if err := model.DB.WithContext(c.Request.Context()).Select("id", "capacity_total", "channel_ratio").First(&channel, channelID).Error; err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	capacity, _, err := channel.RoutingSettings()
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	runtime := service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(channelID))
	runtime.Capacity = capacity
	config, err := model.GetGatewayConfigContext(c.Request.Context())
	if err != nil {
		writeGatewayAPIError(c, err)
		return
	}
	runtime.ConfigVersion = config.Version
	var rate *service.GatewayProviderRateRuntime
	rateStatus := "unavailable"
	if common.RedisEnabled && common.RDB != nil {
		value, err := service.ReadGatewayProviderRateRuntime(c.Request.Context(), common.RDB, service.GatewayChannelRateScopeID(channelID))
		if err == nil {
			rate, rateStatus = &value, "available"
		} else {
			common.SysError(common.LogText("gateway provider runtime read failed: %s", err.Error()))
		}
	}
	common.ApiSuccess(c, struct {
		service.AdmissionSnapshot
		ChannelID  int                                 `json:"channel_id"`
		Rate       *service.GatewayProviderRateRuntime `json:"rate"`
		RateStatus string                              `json:"rate_status"`
	}{runtime, channelID, rate, rateStatus})
}
func ListGatewayChannelCredentials(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil || channelID <= 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid channel ID"), gin.H{"code": "VALIDATION_ERROR"})
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
		writeGatewayAPIError(c, service.ErrGatewayStorageUnavailable)
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
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid user ID"), gin.H{"code": "VALIDATION_ERROR"})
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
		common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("No permission to perform this action"), gin.H{"code": "FORBIDDEN"})
		return user, false
	}
	return user, true
}

func writeGatewayAPIError(c *gin.Context, err error) {
	if errors.Is(err, model.ErrGatewayConfigReference) || errors.Is(err, model.ErrGatewayTPMUnsupported) {
		common.ApiErrorStatus(c, http.StatusBadRequest, err, gin.H{"code": "VALIDATION_ERROR"})
		return
	}
	if errors.Is(err, model.ErrGatewayConfigConflict) {
		common.ApiErrorStatus(c, http.StatusConflict, err, gin.H{"code": "CONFLICT"})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("Gateway resource not found"), gin.H{"code": "NOT_FOUND"})
		return
	}
	common.SysError(common.LogText("gateway administration failed: %s", err.Error()))
	common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Gateway runtime is unavailable"), gin.H{"code": "GATEWAY_UNAVAILABLE"})
}
