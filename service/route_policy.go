package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

func loadRouteChannel(ctx context.Context, channelID int) (model.Channel, error) {
	return loadGatewayChannel(ctx, channelID, false)
}

func loadGatewayChannel(ctx context.Context, channelID int, probe bool) (model.Channel, error) {
	if model.DB == nil || channelID <= 0 {
		return model.Channel{}, ErrAdmissionRuntime
	}
	var channel model.Channel
	if err := model.DB.WithContext(ctx).Select("id", "type", "status", "capacity_total", "channel_ratio").First(&channel, channelID).Error; err != nil {
		return channel, err
	}
	if err := constant.ValidateChannelTypeLifecycle(channel.Type); err != nil {
		return channel, err
	}
	capacity, ratio, err := channel.RoutingSettings()
	if err != nil {
		return channel, err
	}
	channel.CapacityTotal, channel.ChannelRatio = capacity, ratio
	if !gatewayChannelStatusAllowsSend(channel.Status, probe) {
		return channel, ErrAdmissionRuntime
	}
	return channel, nil
}

func gatewayChannelStatusAllowsSend(status int, probe bool) bool {
	return status == common.ChannelStatusEnabled || probe && status == common.ChannelStatusAutoDisabled
}

func GetRouteRuntimeState(ctx context.Context, channelID int, requestModel string) (RouteRuntimeState, error) {
	canonicalModel := modellab.NormalizeModel(requestModel)
	if canonicalModel == "" {
		return RouteRuntimeState{}, ErrAdmissionRuntime
	}
	channel, err := loadRouteChannel(ctx, channelID)
	if err != nil {
		return RouteRuntimeState{}, err
	}
	state := RouteRuntimeState{
		ChannelEnabled: true,
		Capacity:       channel.CapacityTotal, ChannelRatio: channel.ChannelRatio,
	}
	fence, err := model.GetChannelCapabilitySnapshotFence(ctx, channelID)
	if err != nil {
		return RouteRuntimeState{}, err
	}
	state.CapabilityVersion = fence.ActiveVersion
	health, err := LoadRouteHealth(ctx, channelID, canonicalModel)
	if err != nil {
		return RouteRuntimeState{}, err
	}
	state.HealthUnavailable = !RouteHealthReadOnlyUsable(health, time.Now())
	return state, nil
}
