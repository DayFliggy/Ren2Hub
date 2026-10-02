package service

import (
	"context"
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

var providerUsageTaskOnce sync.Once

// Collection is opt-in per channel; an empty database has no enabled entries.
func StartProviderUsageTask() {
	providerUsageTaskOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		common.BackgroundWorkers.Go(func(ctx context.Context) {
			for ctx.Err() == nil {
				if err := collectEnabledProviderUsage(ctx); err != nil && ctx.Err() == nil {
					common.SysError("provider usage collection failed: " + common.MaskSensitiveInfo(err.Error()))
				}
				if !common.WaitBackgroundInterval(ctx, providerUsageInterval) {
					return
				}
			}
		})
	})
}

func collectEnabledProviderUsage(ctx context.Context) error {
	afterID := 0
	var failures []error
	for ctx.Err() == nil {
		ids, err := model.ListProviderUsageEnabledChannels(ctx, afterID, 100)
		if err != nil {
			return errors.Join(append(failures, err)...)
		}
		for _, channelID := range ids {
			afterID = channelID
			channel, _, err := providerUsageChannel(channelID)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if channel.Status != common.ChannelStatusEnabled && channel.Status != common.ChannelStatusAutoDisabled {
				continue
			}
			_, err = defaultProviderUsageCollector.refresh(ctx, channelID, providerUsageInterval)
			if err != nil && !errors.Is(err, model.ErrProviderUsageCooldown) {
				failures = append(failures, err)
			}
		}
		if len(ids) < 100 {
			return errors.Join(failures...)
		}
	}
	return ctx.Err()
}
