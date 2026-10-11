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

// Without Redis the logical-request limit cannot be counted, so it rejects
// with an explained OpenAI error instead of a bare status or a memory count.
func TestModelRateLimitWithoutRedisExplainsRejection(t *testing.T) {
	previousRedis, previousClient := common.RedisEnabled, common.RDB
	previousEnabled := setting.ModelRequestRateLimitEnabled
	common.RedisEnabled, common.RDB = false, nil
	setting.ModelRequestRateLimitEnabled = true
	t.Cleanup(func() {
		common.RedisEnabled, common.RDB = previousRedis, previousClient
		setting.ModelRequestRateLimitEnabled = previousEnabled
	})
	router := gin.New()
	router.POST("/request", func(c *gin.Context) {
		c.Set("id", 83)
		c.Set(common.RequestIdKey, "req-unavailable")
	}, ModelRequestRateLimit(), func(c *gin.Context) {
		t.Error("an uncounted request reached the relay")
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/request", nil))
	assert.Equal(t, http.StatusServiceUnavailable, response.Code)
	want, err := common.Marshal(gin.H{"error": gin.H{
		"message": common.MessageWithRequestId("rate_limit_unavailable", "req-unavailable"),
		"type":    "new_api_error",
		"code":    "",
	}})
	assert.NoError(t, err)
	assert.JSONEq(t, string(want), response.Body.String())
}
