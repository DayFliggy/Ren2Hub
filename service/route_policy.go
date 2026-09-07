package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func loadRouteChannel(ctx context.Context, channelID int) (model.Channel, error) {
	if model.DB == nil || channelID <= 0 {
		return model.Channel{}, ErrRouteLeaseRuntime
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
		return channel, ErrRouteLeaseRuntime
	}
	return channel, nil
}

// AcquireConfiguredRouteLease uses the same channel capacity edited in Vue.
// A channel has one shared pool across all of its models.
func AcquireConfiguredRouteLease(ctx context.Context, requestID string, channelID, userID, tokenID int, requestModel string, ttl time.Duration) (RouteLease, model.Channel, error) {
	channel, err := loadRouteChannel(ctx, channelID)
	if err != nil {
		return RouteLease{}, channel, err
	}
	if userID <= 0 || tokenID < 0 || strings.TrimSpace(requestModel) == "" {
		return RouteLease{}, channel, ErrRouteLeaseRuntime
	}
	if !common.RedisEnabled || common.RDB == nil {
		return RouteLease{}, channel, ErrRouteLeaseUnavailable
	}
	resources := []RouteLeaseResource{{Key: ChannelRouteLeaseKey(channelID), Capacity: channel.CapacityTotal}}
	lease, err := AcquireRouteLease(ctx, common.RDB, requestID, uuid.NewString(), ttl, resources)
	if err != nil {
		return RouteLease{}, channel, err
	}
	lease.ChannelID = channelID
	return lease, channel, nil
}

func ReleaseConfiguredRouteLease(ctx context.Context, lease RouteLease) error {
	if !common.RedisEnabled || common.RDB == nil {
		return ErrRouteLeaseUnavailable
	}
	return ReleaseRouteLease(ctx, common.RDB, lease)
}

func GetRouteRuntimeState(ctx context.Context, channelID int, requestModel string) (RouteLeaseRuntimeState, error) {
	canonicalModel := modellab.NormalizeModel(requestModel)
	if canonicalModel == "" {
		return RouteLeaseRuntimeState{}, ErrRouteLeaseRuntime
	}
	channel, err := loadRouteChannel(ctx, channelID)
	if err != nil {
		return RouteLeaseRuntimeState{}, err
	}
	state := RouteLeaseRuntimeState{
		ChannelEnabled: true, HealthEpoch: 1,
		Capacity: channel.CapacityTotal, ChannelRatio: channel.ChannelRatio,
	}
	fence, err := model.GetChannelCapabilitySnapshotFence(ctx, channelID)
	if err != nil {
		return RouteLeaseRuntimeState{}, err
	}
	state.CapabilityVersion = fence.ActiveVersion
	var health model.ChannelHealth
	err = model.DB.WithContext(ctx).Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, canonicalModel, "").First(&health).Error
	if err == nil {
		state.HealthEpoch = health.HealthEpoch
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return RouteLeaseRuntimeState{}, err
	}
	return state, nil
}
