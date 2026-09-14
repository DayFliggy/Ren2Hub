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
	"github.com/gin-gonic/gin"
)

// GatewayAdmission belongs after authentication and logical request limiting,
// before channel distribution. It owns the user lease for the entire request.
func GatewayAdmission() gin.HandlerFunc {
	return func(c *gin.Context) {
		_, submit, parseErr := getModelRequest(c)
		if parseErr != nil {
			abortWithOpenAiMessage(c, http.StatusBadRequest, parseErr.Error())
			return
		}
		if !submit {
			c.Next()
			return
		}
		userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		if userID <= 0 {
			userID = c.GetInt("id")
		}
		policy, err := service.LoadGatewayAdmissionPolicy(c.Request.Context(), userID)
		if err != nil {
			abortGatewayAdmission(c, err)
			return
		}
		budget := service.NewGatewayWaitBudget(policy.Wait)
		ctx := service.WithGatewayAdmissionContext(c.Request.Context(), policy, budget)
		lease, err := service.AcquireGatewayUserLease(ctx, c.GetString(common.RequestIdKey), userID, policy, budget)
		if err != nil {
			abortGatewayAdmission(c, err)
			return
		}
		if lease.LeaseID == "" {
			c.Request = c.Request.WithContext(ctx)
			c.Next()
			return
		}
		requestCtx, cancel := context.WithCancel(ctx)
		c.Request = c.Request.WithContext(requestCtx)
		renewal := service.StartRouteLeaseRenewal(requestCtx, common.RDB, lease, service.GatewayLeaseRenewInterval, service.GatewayLeaseTTL)
		go func() {
			if err := <-renewal.Done; err != nil {
				cancel()
			}
		}()
		defer func() {
			renewal.Stop()
			<-renewal.Done
			cancel()
			cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cleanupCancel()
			if err := service.ReleaseRouteLease(cleanupCtx, common.RDB, lease); err != nil {
				logger.LogError(c, "gateway user lease release failed: "+err.Error())
			}
			if err := renewal.Failure(); err != nil {
				MarkRelayRequestFailed(c)
				if !c.Writer.Written() {
					abortGatewayAdmission(c, err)
				}
			}
		}()
		c.Next()
	}
}

func abortGatewayAdmission(c *gin.Context, err error) {
	status := http.StatusServiceUnavailable
	if errors.Is(err, service.ErrGatewayQueueFull) || errors.Is(err, service.ErrGatewayWaitExpired) || errors.Is(err, service.ErrRouteLeaseCapacity) {
		status = http.StatusTooManyRequests
	}
	abortWithOpenAiMessage(c, status, service.GatewayAdmissionErrorCode(err))
}
