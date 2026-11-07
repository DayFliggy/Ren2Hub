package model

import "github.com/QuantumNous/new-api/common"

// ChannelStatusUpdateResult separates a durable change from publication so
// Ren2Hub post-commit work does not alter the upstream boolean contract.
type ChannelStatusUpdateResult struct {
	Changed            bool
	PublicationPending bool
}

func UpdateChannelStatusDetailed(channelId int, usingKey string, status int, reason string) ChannelStatusUpdateResult {
	result := ChannelStatusUpdateResult{}
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
	channel, err := GetChannelById(channelId, true)
	if err != nil {
		return result
	} else {
		// Check the stored row too, since this node's cache may be stale.
		if status == common.ChannelStatusAutoDisabled && channel.Status == common.ChannelStatusManuallyDisabled {
			return result
		}
		// A manual channel operation must replace the exhaustion reason even
		// when the status value is already manually disabled.
		overridesKeyExhaustion := channel.ChannelInfo.IsMultiKey && usingKey == "" &&
			status == common.ChannelStatusManuallyDisabled && reason != ChannelStatusReasonAllKeysDisabled &&
			channel.GetOtherInfo()["status_reason"] == ChannelStatusReasonAllKeysDisabled
		if channel.Status == status && !overridesKeyExhaustion {
			return result
		}

		if channel.ChannelInfo.IsMultiKey {
			beforeStatus := channel.Status
			handlerMultiKeyUpdate(channel, usingKey, status, reason)
			if beforeStatus != channel.Status {
				shouldUpdateAbilities = true
			}
		} else {
			info := channel.GetOtherInfo()
			info["status_reason"] = reason
			info["status_time"] = common.GetTimestamp()
			channel.SetOtherInfo(info)
			channel.Status = status
			shouldUpdateAbilities = true
		}
		err = channel.saveStatusState()
		result.Changed = err == nil
		if err != nil {
			common.SysLog(common.LogText("failed to update channel status: channel_id=%d, status=%d, error=%v", channel.Id, status, err))
			if common.MemoryCacheEnabled {
				InitChannelCache()
			}
			return result
		}
		if ChannelRoutingChanged != nil {
			if err := ChannelRoutingChanged(channel.Id); err != nil {
				common.SysLog(common.LogText("failed to publish channel status: channel_id=%d, error=%v", channel.Id, err))
				result.PublicationPending = true
			}
		}
	}
	return result
}
