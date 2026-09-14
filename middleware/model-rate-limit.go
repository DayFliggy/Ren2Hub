package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
)

// ModelRequestRateLimit counts the logical request once. Channel retries reuse
// its success reservation and cannot consume another logical-request slot.
func ModelRequestRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !setting.ModelRequestRateLimitEnabled {
			c.Next()
			return
		}
		if !common.RedisEnabled || common.RDB == nil {
			abortWithOpenAiMessage(c, http.StatusServiceUnavailable, "rate_limit_unavailable")
			return
		}
		totalLimit, successLimit := setting.ModelRequestRateLimitCount, setting.ModelRequestRateLimitSuccessCount
		group := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		if total, success, found := setting.GetGroupRateLimit(group); found {
			totalLimit, successLimit = total, success
		}
		userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		if userID <= 0 {
			userID = c.GetInt("id")
		}
		if userID <= 0 {
			abortWithOpenAiMessage(c, http.StatusUnauthorized, "invalid_user")
			return
		}
		ctx := c.Request.Context()
		window := time.Duration(setting.ModelRequestRateLimitDurationMinutes) * time.Minute
		reservation, err := service.ReserveGatewayRequest(ctx, common.RDB, service.GatewayLogicalRateKey(userID), c.GetString(common.RequestIdKey), window, totalLimit, successLimit)
		if err != nil {
			status := http.StatusServiceUnavailable
			if errors.Is(err, service.ErrGatewayTotalRateLimit) || errors.Is(err, service.ErrGatewaySuccessRateLimit) {
				status = http.StatusTooManyRequests
			}
			abortWithOpenAiMessage(c, status, "rate_limit_exceeded")
			return
		}
		if reservation.ID == "" {
			c.Next()
			return
		}
		requestCtx, cancel := context.WithCancel(ctx)
		c.Request = c.Request.WithContext(requestCtx)
		renewal := service.StartGatewayRequestReservationRenewal(requestCtx, common.RDB, reservation)
		completed := false
		go func() {
			if err := <-renewal.Done; err != nil {
				cancel()
			}
		}()
		defer func() {
			renewal.Stop()
			<-renewal.Done
			success := completed && ctx.Err() == nil && c.Writer.Status() < http.StatusBadRequest && !c.GetBool(relayRequestFailedKey) && renewal.Failure() == nil
			cancel()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cleanupCancel()
			if err := service.FinishGatewayRequestReservation(cleanupCtx, common.RDB, reservation, success); err != nil {
				MarkRelayRequestFailed(c)
				logger.LogError(c, "gateway success reservation finalization failed: "+err.Error())
				if !c.Writer.Written() {
					abortWithOpenAiMessage(c, http.StatusServiceUnavailable, "rate_limit_unavailable")
				}
			}
		}()
		c.Next()
		completed = true
	}
}
