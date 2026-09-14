package middleware

import (
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

func TestGatewayAdmissionOwnsUserLeaseAndReleasesOnFailure(t *testing.T) {
	_, client := useRateLimitMiniRedis(t)
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
		assert.Equal(t, int64(1), client.ZCard(t.Context(), service.UserRouteLeaseKey(73)).Val())
		policy, budget, ok := service.GatewayAdmissionFromContext(c.Request.Context())
		assert.True(t, ok)
		assert.Equal(t, 1, policy.UserConcurrency)
		assert.Zero(t, budget.Remaining())
		c.Status(http.StatusBadGateway)
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/request", nil))
	assert.Equal(t, http.StatusBadGateway, response.Code)
	assert.Zero(t, client.ZCard(t.Context(), service.UserRouteLeaseKey(73)).Val())
	common.RedisEnabled = false
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/request", nil))
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
}
