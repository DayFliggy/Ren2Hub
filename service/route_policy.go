package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

func loadRouteChannel(ctx context.Context, channelID int) (model.Channel, error) {
	if model.DB == nil || channelID <= 0 {
		return model.Channel{}, ErrAdmissionRuntime
	}
	var channel model.Channel
	if err := model.DB.WithContext(ctx).Select("id", "status", "capacity_total", "channel_ratio").First(&channel, channelID).Error; err != nil {
		return channel, err
	}
	capacity, ratio, err := channel.RoutingSettings()
	if err != nil {
		return channel, err
	}
	channel.CapacityTotal, channel.ChannelRatio = capacity, ratio
	if channel.Status != common.ChannelStatusEnabled {
		return channel, ErrAdmissionRuntime
	}
	return channel, nil
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
