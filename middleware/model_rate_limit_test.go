package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestModelRateLimitFailureReleasesSuccessReservation(t *testing.T) {
	_, client := useRateLimitMiniRedis(t)
	previousRedis, previousClient := common.RedisEnabled, common.RDB
	previousEnabled, previousCount, previousSuccess, previousDuration := setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes
	common.RedisEnabled, common.RDB = true, client
	setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes = true, 3, 1, 1
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousRedis, previousClient
		setting.ModelRequestRateLimitEnabled, setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount, setting.ModelRequestRateLimitDurationMinutes = previousEnabled, previousCount, previousSuccess, previousDuration
	})
	router := gin.New()
	number := 0
	router.Use(func(c *gin.Context) {
		number++
		c.Set("id", 82)
		c.Set(common.RequestIdKey, fmt.Sprint(number))
		c.Next()
	})
	router.Use(ModelRequestRateLimit())
	router.POST("/request", func(c *gin.Context) {
		if number == 1 {
			MarkRelayRequestFailed(c)
		}
		c.Status(http.StatusOK)
	})
	for _, status := range []int{200, 200, 429, 429} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/request", nil))
		assert.Equal(t, status, response.Code)
	}
	assert.Equal(t, int64(1), client.ZCard(t.Context(), service.GatewayLogicalRateKey(82)+":success").Val())
	assert.Zero(t, client.ZCard(t.Context(), service.GatewayLogicalRateKey(82)+":pending").Val())
}
