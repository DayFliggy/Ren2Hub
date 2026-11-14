package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ChannelStatusUpdateResult separates a durable change from publication so
// Ren2Hub post-commit work does not alter the upstream boolean contract.
type ChannelStatusUpdateResult struct {
	Changed            bool
	PublicationPending bool
}

func UpdateChannelStatusDetailed(channelId int, usingKey string, status int, reason string) (result ChannelStatusUpdateResult) {
	// A status set by hand names no key and is never an automatic disable; it
	// reaches every node within seconds. An automatic change, a disable after a
	// failure or the re-enable after a test of the key it used, is left to the
	// periodic channel sync: the keys of busy channels change status often, and
	// each bump reloads the whole channel cache on every node.
	bump := usingKey == "" && status != common.ChannelStatusAutoDisabled
	// Runs after the ability update deferred below.
	defer func() {
		if result.Changed && bump {
			BumpSyncTopic(SyncTopicChannels)
		}
	}()
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}

	// ChannelInfo stores both multi-key status and the polling cursor. Hold the
	// same per-channel lock from the first read through persistence so neither
	// writer can save a stale JSON snapshot over the other.
	pollingLock := GetChannelPollingLock(channelId)
	pollingLock.Lock()
	defer pollingLock.Unlock()

	if common.MemoryCacheEnabled {
		channelCache, _ := CacheGetChannel(channelId)
		if channelCache == nil {
			return result
		}
		// A request failure must not replace an admin's manual disable;
		// otherwise automatic recovery could re-enable the channel later.
		if status == common.ChannelStatusAutoDisabled && channelCache.Status == common.ChannelStatusManuallyDisabled {
			return result
		}
		if channelCache.ChannelInfo.IsMultiKey {
			beforeStatus := channelCache.Status
			// 如果是多Key模式，更新缓存中的状态
			handlerMultiKeyUpdate(channelCache, usingKey, status, reason)
			if beforeStatus != channelCache.Status {
				CacheUpdateChannelStatus(channelId, channelCache.Status)
			}
			//CacheUpdateChannel(channelCache)
			//return true
		} else {
			// 如果缓存渠道存在，且状态已是目标状态，直接返回
			if channelCache.Status == status {
				return result
			}
			CacheUpdateChannelStatus(channelId, status)
		}
	}

	shouldUpdateAbilities := false
	defer func() {
		if shouldUpdateAbilities {
			err := UpdateAbilityStatus(channelId, status == common.ChannelStatusEnabled)
			if err != nil {
				common.SysLog(common.LogText("failed to update ability status: channel_id=%d, error=%v", channelId, err))
			}
		}
	}()
	// The status, the key statuses and the status reason are written from the
	// stored row, keeping what another writer stores in other_info meanwhile.
	written, err := UpdateChannelOtherInfo(channelId, func(channel *Channel) (map[string]any, error) {
		shouldUpdateAbilities = false
		// Check the stored row too, since this node's cache may be stale.
		if status == common.ChannelStatusAutoDisabled && channel.Status == common.ChannelStatusManuallyDisabled {
			return nil, nil
		}
		// A manual channel operation must replace the exhaustion reason even
		// when the status value is already manually disabled.
		overridesKeyExhaustion := channel.ChannelInfo.IsMultiKey && usingKey == "" &&
			status == common.ChannelStatusManuallyDisabled && reason != ChannelStatusReasonAllKeysDisabled &&
			channel.GetOtherInfo()["status_reason"] == ChannelStatusReasonAllKeysDisabled
		if channel.Status == status && !overridesKeyExhaustion {
			return nil, nil
		}

		if channel.ChannelInfo.IsMultiKey {
			beforeStatus := channel.Status
			handlerMultiKeyUpdate(channel, usingKey, status, reason)
			shouldUpdateAbilities = beforeStatus != channel.Status
		} else {
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			channel.Status = status
			shouldUpdateAbilities = true
		}
		updates := map[string]any{"status": channel.Status}
		if channel.ChannelInfo.IsMultiKey {
			updates["channel_info"] = channel.ChannelInfo
		}
		return updates, nil
	})
	shouldUpdateAbilities = shouldUpdateAbilities && written
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		common.SysLog(common.LogText("failed to update channel status: channel_id=%d, status=%d, error=%v", channelId, status, err))
	}
	result.Changed = written
	if err != nil {
		return result
	}
	if result.Changed && ChannelRoutingChanged != nil {
		if err := ChannelRoutingChanged(channelId); err != nil {
			common.SysLog(common.LogText("failed to publish channel status: channel_id=%d, error=%v", channelId, err))
			result.PublicationPending = true
		}
	}
	return result
}
