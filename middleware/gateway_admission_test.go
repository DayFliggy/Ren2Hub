package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayAdmissionErrorResponseIdentifiesResource(t *testing.T) {
	for _, scenario := range []struct {
		name, code, resource string
		cause                error
		status               int
	}{
		{"user", "gateway_capacity_exhausted", "user", service.ErrAdmissionCapacity, 429},
		{"process", "gateway_total_queue_full", "process", service.ErrGatewayTotalQueueFull, 429},
		{"draining", "gateway_draining", "user", service.ErrGatewayDraining, 503},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			abortGatewayAdmission(c, scenario.cause)
			assert.Equal(t, scenario.status, response.Code)
			var body struct {
				Error struct{ Code, Message string }
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, scenario.code, body.Error.Code)
			assert.Contains(t, body.Error.Message, scenario.resource+" admission:")
		})
	}
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	abortGatewayAdmission(c, service.ErrGatewayDraining)
	assert.Empty(t, response.Body.String(), "disconnected clients must not receive another error")
}

func TestGatewayAdmissionOwnsUserPermitAndReleasesOnFailure(t *testing.T) {
	useRateLimitMiniRedis(t)
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}))
	one, zero := 1, 0
	require.NoError(t, db.Create(&model.User{Id: 73, Username: "gateway-admission-test", Status: common.UserStatusEnabled, GatewayConcurrency: &one, GatewayWaitMS: &zero}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 73); c.Set(common.RequestIdKey, "request"); c.Next() })
	router.Use(GatewayAdmission())
	router.POST("/request", func(c *gin.Context) {
		assert.Equal(t, int64(1), service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(73)).InFlight)
		policy, budget, ok := service.GatewayAdmissionFromContext(c.Request.Context())
		assert.True(t, ok)
		assert.Equal(t, 1, policy.UserConcurrency)
		assert.Zero(t, budget.Remaining())
		c.Status(http.StatusBadGateway)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/request", nil))
	assert.Equal(t, http.StatusBadGateway, response.Code)
	assert.Zero(t, service.ReadGatewayAdmissionRuntime(service.UserAdmissionKey(73)).InFlight)
	common.RedisEnabled = false
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/request", nil))
	assert.Equal(t, http.StatusBadGateway, response.Code)
}
