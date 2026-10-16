package middleware

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GatewayAdmission belongs after authentication and logical request limiting,
// before channel distribution. It owns the user permit for the entire logical request.
func GatewayAdmission() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, header := range []string{"X-Ren2Hub-Trace-Id", "X-Ren2Hub-Thread-Id"} {
			values := c.Request.Header.Values(header)
			if len(values) > 1 || len(values) == 1 && !service.ValidRequestCorrelationID(values[0]) {
				abortWithOpenAiMessage(c, http.StatusBadRequest, "invalid request correlation identifier")
				return
			}
			if len(values) == 1 {
				c.Set(header, values[0])
			}
		}
		_, _, parseErr := getModelRequest(c)
		if parseErr != nil {
			abortInvalidRequest(c, parseErr)
			return
		}
		if !requiresGatewayAdmission(c) {
			c.Next()
			return
		}
		userID := common.GetContextKeyInt(c, constant.ContextKeyUserId)
		if userID <= 0 {
			userID = c.GetInt("id")
		}
		// Register before acquisition so a request admitted just before drain can
		// complete, and so shutdown also observes in-progress policy reads.
		ctx, finish, err := service.TrackGatewayRequest(c.Request.Context())
		if err != nil {
			abortGatewayAdmission(c, err)
			return
		}
		defer finish()
		c.Request = c.Request.WithContext(ctx)
		policy, err := service.LoadGatewayAdmissionPolicy(c.Request.Context(), userID)
		if err != nil {
			abortGatewayAdmission(c, err)
			return
		}
		budget := service.NewGatewayWaitBudget(policy.Wait)
		ctx = service.WithGatewayAdmissionContext(c.Request.Context(), policy, budget)
		permit, err := service.AcquireGatewayUserPermit(ctx, c.GetString(common.RequestIdKey), userID, policy, budget)
		if err != nil {
			abortGatewayAdmission(c, err)
			return
		}
		defer permit.Release()
		c.Set("gateway_user_queue_ms", permit.QueueWaitMS())
		c.Request = c.Request.WithContext(ctx)
		c.Writer = &gatewayTrackedWriter{ResponseWriter: c.Writer, ctx: ctx}
		c.Next()
	}
}

type gatewayTrackedWriter struct {
	gin.ResponseWriter
	ctx context.Context
}

func (writer *gatewayTrackedWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := writer.ResponseWriter.Hijack()
	if err == nil {
		service.TrackGatewayConnection(writer.ctx, conn)
	}
	return conn, rw, err
}

func abortGatewayAdmission(c *gin.Context, err error) {
	if c.Request.Context().Err() != nil || c.Writer.Written() {
		c.Abort()
		return
	}
	code := service.GatewayAdmissionErrorCode(err)
	resource := "user"
	if errors.Is(err, service.ErrGatewayTotalQueueFull) {
		resource = "process"
	}
	abortWithOpenAiMessage(c, service.GatewayAdmissionHTTPStatus(err), resource+" admission: "+code, types.ErrorCode(code))
}
