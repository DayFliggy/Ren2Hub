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
	// Runs after the status and abilities are written and published.
	defer func() {
		if result.Changed && bump {
			BumpSyncTopic(SyncTopicChannels)
		}
	}()
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
		channelCache, _ := CacheGetChannel(channelId)
		if channelCache == nil {
			return result
		}
		// A request failure must not replace an admin's manual disable;
		// otherwise automatic recovery could re-enable the channel later.
		if status == common.ChannelStatusAutoDisabled && channelCache.Status == common.ChannelStatusManuallyDisabled {
			return result
		}
		// 如果缓存渠道存在，且状态已是目标状态，直接返回
		if channelCache.Status == status {
			return result
		}
		CacheUpdateChannelStatus(channelId, status)
	}

	written, err := updateChannelStatusLocked(channelId, status, reason)
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

// updateChannelStatusLocked writes the status, its reason and the channel's
// abilities in one transaction under the channel row lock (SQLite: the
// channel key lock), the lock every writer of a channel's key or status
// holds, so the channel and its abilities always agree. Another writer of
// other_info finds it changed and reads it again.
func updateChannelStatusLocked(channelID, status int, reason string) (written bool, err error) {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lock := sqliteChannelKeyLock(channelID)
		lock.Lock()
		defer lock.Unlock()
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).Where("id = ?", channelID).First(&channel).Error; err != nil {
			return err
		}
		// Check the stored row too, since this node's cache may be stale.
		if status == common.ChannelStatusAutoDisabled && channel.Status == common.ChannelStatusManuallyDisabled {
			return nil
		}
		if channel.Status == status {
			return nil
		}
		info := channel.GetOtherInfo()
		info["status_reason"] = reason
		info["status_time"] = common.GetTimestamp()
		channel.SetOtherInfo(info)
		updates := map[string]any{"status": status, "other_info": channel.OtherInfo}
		if err := tx.Model(&Channel{}).Where("id = ?", channelID).Updates(updates).Error; err != nil {
			return err
		}
		written = true
		return writeAbilityStatus(tx, channelID, status)
	})
	return written && err == nil, err
}

// writeAbilityStatus writes a channel's new status to its abilities inside
// the transaction that writes the channel.
func writeAbilityStatus(tx *gorm.DB, channelID, status int) error {
	return updateAbilityStatus(tx.Where("channel_id = ?", channelID), status == common.ChannelStatusEnabled)
}
