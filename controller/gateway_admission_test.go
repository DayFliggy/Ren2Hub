package controller

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupGatewayAPI(t *testing.T) (*gorm.DB, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}, &model.Channel{}, &model.GatewayCredential{}, &model.Log{}))
	previousDB, previousLogDB := model.DB, model.LOG_DB
	model.DB, model.LOG_DB = db, db
	previousRedis, previousRedisEnabled := common.RDB, common.RedisEnabled
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.RDB, common.RedisEnabled = previousRedis, previousRedisEnabled
		require.NoError(t, client.Close())
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	encoded, err := common.Marshal(model.DefaultGatewayConfig())
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
	require.NoError(t, db.Create(&model.User{Id: 1, Username: "gateway-user", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Group: "team"}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleRootUser) })
	router.GET("/self", GetGatewayUserAdmission)
	router.GET("/self/runtime", GetGatewayUserRuntime)
	router.GET("/config", GetGatewayConfig)
	router.PUT("/config", UpdateGatewayConfig)
	router.PUT("/users/:id", UpdateGatewayUserAdmission)
	router.GET("/channels/:id/credentials", ListGatewayChannelCredentials)
	return db, router
}

func gatewayAPIRequest(t *testing.T, router *gin.Engine, method, path string, input any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := common.Marshal(input)
	require.NoError(t, err)
	request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestGatewayAPIReportsInheritedSourcesAndExplicitZero(t *testing.T) {
	db, router := setupGatewayAPI(t)
	config := model.DefaultGatewayConfig()
	groupLimit, zero := 4, 0
	config.Groups["team"] = model.GatewayAdmissionOverride{Concurrency: &groupLimit, WaitMS: &zero}
	require.NoError(t, model.SaveGatewayConfig(config))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Update("gateway_concurrency", 0).Error)
	response := gatewayAPIRequest(t, router, http.MethodGet, "/self", nil)
	require.Equal(t, http.StatusOK, response.Code)
	var result struct {
		Data struct {
			Effective gatewayEffectiveAdmission       `json:"effective"`
			Sources   service.GatewayAdmissionSources `json:"sources"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.Equal(t, gatewayEffectiveAdmission{Concurrency: 0, WaitMS: 0, QueueSize: 20, ChannelQueueSize: 20}, result.Data.Effective)
	assert.Equal(t, service.GatewayAdmissionSources{Concurrency: "user", WaitMS: "group", QueueSize: "system", ChannelQueueSize: "system"}, result.Data.Sources)
}

func TestGatewayAPIConfigVersionAndProviderTPM(t *testing.T) {
	db, router := setupGatewayAPI(t)
	require.NoError(t, db.Create(&model.Channel{Id: 2, Type: constant.ChannelTypeOpenAI, Key: "test-only-credential", Name: "gateway-channel"}).Error)
	identity, err := model.EnsureGatewayCredential(context.Background(), 2, "test-only-credential")
	require.NoError(t, err)
	config := model.DefaultGatewayConfig()
	config.ChannelRates[2] = model.GatewayProviderRateLimit{RPM: 2, TPM: 50}
	config.CredentialRates[identity.ID] = model.GatewayProviderRateLimit{RPM: 1, TPM: 40}
	response := gatewayAPIRequest(t, router, http.MethodPut, "/config", config)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Contains(t, response.Body.String(), `"version":2`)
	response = gatewayAPIRequest(t, router, http.MethodPut, "/config", config)
	assert.Equal(t, http.StatusConflict, response.Code)
	scopes, err := service.LoadGatewayProviderRateScopes(context.Background(), 2, identity.ID)
	require.NoError(t, err)
	_, err = service.ReserveGatewayProviderRate(context.Background(), common.RDB, scopes, 41)
	require.ErrorIs(t, err, service.ErrGatewayProviderRateLimit)
	_, err = service.ReserveGatewayProviderRate(context.Background(), common.RDB, scopes, 40)
	require.NoError(t, err)
	config.Version = 2
	config.ChannelRates[2] = model.GatewayProviderRateLimit{TPM: -1}
	response = gatewayAPIRequest(t, router, http.MethodPut, "/config", config)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	config.ChannelRates = map[int]model.GatewayProviderRateLimit{999: {TPM: 30}}
	response = gatewayAPIRequest(t, router, http.MethodPut, "/config", config)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestGatewayAPIRejectsTPMForUnboundedChannelAndCredential(t *testing.T) {
	db, router := setupGatewayAPI(t)
	require.NoError(t, db.Create(&model.Channel{Id: 3, Type: constant.ChannelTypeSora, Key: "media-only-fixture"}).Error)
	credential, err := model.EnsureGatewayCredential(context.Background(), 3, "media-only-fixture")
	require.NoError(t, err)
	for _, channelType := range []int{constant.ChannelTypeSora, constant.ChannelTypeReplicate, constant.ChannelTypeCoze} {
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 3).Update("type", channelType).Error)
		for _, credentialScope := range []bool{false, true} {
			config := model.DefaultGatewayConfig()
			if credentialScope {
				config.CredentialRates[credential.ID] = model.GatewayProviderRateLimit{TPM: 10}
			} else {
				config.ChannelRates[3] = model.GatewayProviderRateLimit{TPM: 10}
			}
			response := gatewayAPIRequest(t, router, http.MethodPut, "/config", config)
			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.Contains(t, response.Body.String(), "TPM requires")
			stored, err := model.GetGatewayConfig()
			require.NoError(t, err)
			assert.Equal(t, int64(1), stored.Version)
		}
	}
	config := model.DefaultGatewayConfig()
	config.ChannelRates[3] = model.GatewayProviderRateLimit{RPM: 10}
	response := gatewayAPIRequest(t, router, http.MethodPut, "/config", config)
	assert.Equal(t, http.StatusOK, response.Code)
}

func TestGatewayAPIUserUpdateIsIdempotentAndCanRestoreInheritance(t *testing.T) {
	db, router := setupGatewayAPI(t)
	zero := 0
	input := model.GatewayAdmissionOverride{Concurrency: &zero, WaitMS: &zero, QueueSize: &zero}
	response := gatewayAPIRequest(t, router, http.MethodPut, "/users/1", input)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	// SQLite counts matching rows; emulate MySQL's zero changed-row result.
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("gateway_noop_rows", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" {
			tx.RowsAffected = 0
		}
	}))
	response = gatewayAPIRequest(t, router, http.MethodPut, "/users/1", input)
	assert.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, db.Callback().Update().Remove("gateway_noop_rows"))
	response = gatewayAPIRequest(t, router, http.MethodPut, "/users/1", model.GatewayAdmissionOverride{})
	require.Equal(t, http.StatusOK, response.Code)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Nil(t, user.GatewayConcurrency)
	assert.Nil(t, user.GatewayWaitMS)
	assert.Nil(t, user.GatewayQueueSize)
}

func TestGatewayAPIRuntimeUnavailableAndCredentialListRedaction(t *testing.T) {
	db, router := setupGatewayAPI(t)
	require.NoError(t, db.Create(&model.Channel{Id: 2, Key: "test-only-secret", Name: "gateway-channel"}).Error)
	identity, err := model.EnsureGatewayCredential(context.Background(), 2, "test-only-secret")
	require.NoError(t, err)
	response := gatewayAPIRequest(t, router, http.MethodGet, "/channels/2/credentials", nil)
	require.Equal(t, http.StatusOK, response.Code)
	assert.JSONEq(t, fmt.Sprintf(`{"success":true,"message":"","data":[{"id":%q}]}`, identity.ID), response.Body.String())
	common.RedisEnabled = false
	response = gatewayAPIRequest(t, router, http.MethodGet, "/self/runtime", nil)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"in_flight":0`)
}

func TestGatewayAPIAdminCannotOverridePeer(t *testing.T) {
	db, _ := setupGatewayAPI(t)
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "gateway-admin", AffCode: "gateway-admin", Role: common.RoleAdminUser}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleAdminUser) })
	router.PUT("/users/:id", UpdateGatewayUserAdmission)
	response := gatewayAPIRequest(t, router, http.MethodPut, "/users/2", model.GatewayAdmissionOverride{})
	assert.Equal(t, http.StatusForbidden, response.Code)
}

func TestGatewayAPIKeepsNewSettingsForOldClientsAndRejectsNull(t *testing.T) {
	_, router := setupGatewayAPI(t)
	t.Cleanup(func() {
		current, err := model.GetGatewayConfig()
		require.NoError(t, err)
		defaults := model.DefaultGatewayConfig()
		defaults.Version = current.Version
		require.NoError(t, model.SaveGatewayConfig(defaults))
	})
	config := model.DefaultGatewayConfig()
	config.TotalQueueSize, config.RetryBudgetEnabled, config.RetryBudgetPercent, config.RetryBudgetBurst = 0, false, 7, 9
	response := gatewayAPIRequest(t, router, http.MethodPut, "/config", config)
	require.Equal(t, http.StatusOK, response.Code)
	encoded, err := common.Marshal(config)
	require.NoError(t, err)
	var legacy map[string]interface{}
	require.NoError(t, common.Unmarshal(encoded, &legacy))
	for _, field := range []string{"total_queue_size", "retry_budget_enabled", "retry_budget_percent", "retry_budget_burst"} {
		delete(legacy, field)
	}
	legacy["version"], legacy["user_concurrency"] = 2, 3
	response = gatewayAPIRequest(t, router, http.MethodPut, "/config", legacy)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	stored, err := model.GetGatewayConfig()
	require.NoError(t, err)
	assert.Zero(t, stored.TotalQueueSize)
	assert.False(t, stored.RetryBudgetEnabled)
	assert.Equal(t, 7, stored.RetryBudgetPercent)
	assert.Equal(t, 9, stored.RetryBudgetBurst)
	assert.Equal(t, 3, stored.UserConcurrency)
	for _, field := range []string{"total_queue_size", "retry_budget_enabled", "retry_budget_percent", "retry_budget_burst"} {
		legacy["version"], legacy[field] = 3, nil
		response = gatewayAPIRequest(t, router, http.MethodPut, "/config", legacy)
		assert.Equal(t, http.StatusBadRequest, response.Code, field)
		delete(legacy, field)
	}
	current, err := model.GetGatewayConfig()
	require.NoError(t, err)
	assert.Equal(t, stored, current)
}

func TestGatewayAPIChannelRuntimeKeepsLocalValuesWhenRateUnavailable(t *testing.T) {
	db, router := setupGatewayAPI(t)
	require.NoError(t, db.Create(&model.Channel{Id: 25, Status: common.ChannelStatusEnabled, CapacityTotal: 2}).Error)
	router.GET("/runtime/channels/:id", GetGatewayChannelRuntime)
	permit, _, err := service.AcquireGatewayChannelPermit(context.Background(), "runtime-fixture", 25, false, nil)
	require.NoError(t, err)
	defer permit.Release()
	common.RedisEnabled = false
	response := gatewayAPIRequest(t, router, http.MethodGet, "/runtime/channels/25", nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct {
		Data struct {
			InFlight int64       `json:"in_flight"`
			Rate     interface{} `json:"rate"`
			Status   string      `json:"rate_status"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	assert.EqualValues(t, 1, result.Data.InFlight)
	assert.Nil(t, result.Data.Rate)
	assert.Equal(t, "unavailable", result.Data.Status)
}
