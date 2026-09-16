package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"gorm.io/gorm"
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
		ChannelEnabled: true, HealthEpoch: 1,
		Capacity: channel.CapacityTotal, ChannelRatio: channel.ChannelRatio,
	}
	fence, err := model.GetChannelCapabilitySnapshotFence(ctx, channelID)
	if err != nil {
		return RouteRuntimeState{}, err
	}
	state.CapabilityVersion = fence.ActiveVersion
	var health model.ChannelHealth
	err = model.DB.WithContext(ctx).Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, canonicalModel, "").First(&health).Error
	if err == nil {
		state.HealthEpoch = health.HealthEpoch
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return RouteRuntimeState{}, err
	}
	return state, nil
}
