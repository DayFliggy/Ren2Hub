package controller

import (
	"context"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// A probe has its own administrative eligibility, while capacity, health
// ownership, provider limits and the persistent send boundary are shared.
func prepareChannelTestAdmission(c *gin.Context, info *relaycommon.RelayInfo) (func(), error) {
	ctx, finish, err := service.TrackGatewayRequest(c.Request.Context())
	if err != nil {
		return nil, err
	}
	policy, err := service.LoadGatewayAdmissionPolicy(ctx, info.UserId)
	if err != nil {
		finish()
		return nil, err
	}
	budget := service.NewGatewayWaitBudget(policy.Wait)
	ctx = service.WithGatewayAdmissionContext(ctx, policy, budget)
	userPermit, err := service.AcquireGatewayUserPermit(ctx, info.RequestId, info.UserId, policy, budget)
	if err != nil {
		finish()
		return nil, err
	}
	cleanup := func() { userPermit.Release(); finish() }
	channelPermit, _, err := service.AcquireGatewayChannelProbePermit(ctx, info.RequestId, info.ChannelId, budget)
	if err != nil {
		cleanup()
		return nil, err
	}
	c.Request = c.Request.WithContext(ctx)
	if err := service.ValidateGatewayChannelProbeIdentity(c, info.ChannelId); err != nil {
		channelPermit.Release()
		cleanup()
		return nil, err
	}
	c.Set(common.RequestIdKey, info.RequestId)
	c.Set("route_channel_permit", channelPermit)
	info.RouteCapabilityModel = info.OriginModelName
	info.DeferBillingToSend = true
	state := &routeAttemptHealthState{}
	c.Set("route_live_health", state)
	info.BeforeSend = func(body io.Reader) error {
		// The account may have been disabled while this probe waited in FIFO.
		if _, err := service.LoadGatewayAdmissionPolicy(c.Request.Context(), info.UserId); err != nil {
			return err
		}
		return prepareLiveRouteSend(c, info, state, uuid.NewString(), body)
	}
	startRouteAttemptMaintenance(c, state)
	return cleanup, nil
}

func startRouteAttemptMaintenance(c *gin.Context, state *routeAttemptHealthState) {
	parent := c.Request.Context()
	ctx, cancel := context.WithCancel(parent)
	c.Request = c.Request.WithContext(ctx)
	renewal := service.StartAttemptMaintenance(ctx, 30*time.Second, state.renew)
	go func() {
		if err, ok := <-renewal.Done; ok && err != nil {
			cancel()
		}
	}()
	c.Set("route_attempt_cancel", cancel)
	c.Set("route_attempt_parent_context", parent)
	c.Set("route_live_renewal", renewal)
}
