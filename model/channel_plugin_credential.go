package model

import (
	"context"
	"errors"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Fixed reasons the host records when it disables a task plugin channel, or
// one of its keys, for a credential only a login can restore. A successful
// login re-enables what was disabled for one of them; a manual disable stays.
const (
	ChannelStatusReasonPluginLoginRequired = "Plugin credential login required"
	ChannelStatusReasonPluginNeedsLogin    = "Plugin credential needs a new login"
)

// ErrChannelKeyChanged reports a channel key that changed while a locked
// update waited, which only a write outside UpdateChannelLocked can do.
var ErrChannelKeyChanged = errors.New("channel key changed during the credential update")

// SQLite serves one node, so process locks stand in for its row locks.
var (
	sqliteChannelKeyLocks sync.Map // channel id -> *sync.Mutex
	sqlitePluginLoginLock sync.Mutex
)

func sqliteChannelKeyLock(channelID int) *sync.Mutex {
	lock, _ := sqliteChannelKeyLocks.LoadOrStore(channelID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// UpdateChannelLocked lets update change a channel's key while no other node
// or goroutine can: MySQL and PostgreSQL hold the channel row lock across
// update, and SQLite holds a process lock. update must not wait on the
// network, because the channel's other writers wait on this lock.
// Every write of a channel's key goes through here or through
// UpdateTaskPluginChannelLocked, which takes the same locks; Channel.Update
// never writes it. So the key update reads is the stored one, never a copy a
// refresh has replaced since.
// update receives the stored channel and may change its Key, Status and
// OtherInfo; the changed ones are written together. The
// stored channel afterwards is returned, a new key is published to this
// node's channel cache, and a new status to the channel's abilities.
func UpdateChannelLocked(channelID int, update func(channel *Channel) error) (*Channel, error) {
	channel, written, err := updateChannelStateAndCache(channelID, update)
	if err != nil {
		return nil, err
	}
	if written {
		defer BumpSyncTopic(SyncTopicChannels)
		if err := publishChannelRoutingIDs([]int{channelID}); err != nil {
			return nil, err
		}
	}
	return channel, nil
}

func updateChannelStateAndCache(channelID int, update func(*Channel) error) (*Channel, bool, error) {
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}
	var channel Channel
	var keyChanged, statusChanged, written bool
	// changes runs update on channel and returns the columns it changed.
	changes := func() (map[string]any, error) {
		key, status, otherInfo := channel.Key, channel.Status, channel.OtherInfo
		if err := update(&channel); err != nil {
			return nil, err
		}
		updates := map[string]any{}
		if channel.Key != key {
			updates["key"], keyChanged = channel.Key, true
		}
		if channel.Status != status {
			updates["status"], statusChanged = channel.Status, true
		}
		if channel.OtherInfo != otherInfo {
			updates["other_info"] = channel.OtherInfo
		}
		return updates, nil
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lock := sqliteChannelKeyLock(channelID)
		lock.Lock()
		defer lock.Unlock()
		if err := DB.Where("id = ?", channelID).First(&channel).Error; err != nil {
			return nil, false, err
		}
		stored := channel.Key
		updates, err := changes()
		if err != nil {
			if mismatch, ok := err.(*upstreamBindingMismatch); ok {
				err = validateUpstreamBindingDB(mismatch.binding)
				if err == nil {
					err = ErrUpstreamRatioConflict
				}
			}
			return nil, false, err
		}
		if len(updates) > 0 {
			err := DB.Session(&gorm.Session{Logger: logger.Discard}).Transaction(func(tx *gorm.DB) error {
				result := tx.Model(&Channel{}).Where("id = ?", channelID).Where(map[string]any{"key": stored}).Updates(updates)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return ErrChannelKeyChanged
				}
				if statusChanged {
					return writeAbilityStatus(tx, channelID, channel.Status)
				}
				return nil
			})
			if err != nil {
				return nil, false, err
			}
			written = true
		}
	} else {
		err := channelBindingTransaction(DB, func(tx *gorm.DB) error {
			if err := lockForUpdate(tx).Where("id = ?", channelID).First(&channel).Error; err != nil {
				return err
			}
			updates, err := changes()
			if err != nil || len(updates) == 0 {
				return err
			}
			written = true
			if err := tx.Model(&Channel{}).Where("id = ?", channelID).Updates(updates).Error; err != nil {
				return err
			}
			if statusChanged {
				return writeAbilityStatus(tx, channelID, channel.Status)
			}
			return nil
		})
		if err != nil {
			return nil, false, err
		}
	}
	if keyChanged {
		CacheUpdateChannelKey(channelID, channel.Key)
	}
	if statusChanged {
		CacheUpdateChannelStatus(channelID, channel.Status)
	}
	return &channel, written, nil
}

// UpdateTaskPluginChannelLocked runs update on a task plugin channel while
// every other task plugin channel is locked too, so a check across channels,
// such as one account per plugin, cannot interleave with another login's
// write. update may change Key, Status and OtherInfo and must not wait on
// the network. The caller reloads the channel cache.
func UpdateTaskPluginChannelLocked(channelID int, update func(channel *Channel, others []*Channel) error) error {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		sqlitePluginLoginLock.Lock()
		defer sqlitePluginLoginLock.Unlock()
		keyLock := sqliteChannelKeyLock(channelID)
		keyLock.Lock()
		defer keyLock.Unlock()
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		var channels []*Channel
		if err := lockForUpdate(tx).Where("type = ?", constant.ChannelTypeTaskPlugin).Order("id").Find(&channels).Error; err != nil {
			return err
		}
		var target *Channel
		others := make([]*Channel, 0, len(channels))
		for _, channel := range channels {
			if channel.Id == channelID {
				target = channel
				continue
			}
			others = append(others, channel)
		}
		if target == nil {
			return gorm.ErrRecordNotFound
		}
		status := target.Status
		if err := update(target, others); err != nil {
			return err
		}
		updates := map[string]any{
			"key":        target.Key,
			"status":     target.Status,
			"other_info": target.OtherInfo,
		}
		if err := tx.Model(&Channel{}).Where("id = ?", channelID).Updates(updates).Error; err != nil {
			return err
		}
		if target.Status != status {
			return writeAbilityStatus(tx, channelID, target.Status)
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer BumpSyncTopic(SyncTopicChannels)
	return publishChannelRoutingIDs([]int{channelID})
}

// CacheUpdateChannelKey publishes a new key for a cached channel. The cached
// channel is replaced by a copy, so a request holding the previous one keeps
// reading a consistent key.
func CacheUpdateChannelKey(id int, key string) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	cached, ok := channelsIDM[id]
	if !ok {
		return
	}
	updated := *cached
	updated.Key = key
	channelsIDM[id] = &updated
}

// channelRelayConfig returns the relay routes of a task plugin channel's
// plugin, nil for any other channel. It reads the setting without GetSetting,
// which clears the stored setting when it is invalid, so a pricing read never
// writes the channel.
func channelRelayConfig(ch *Channel) *dto.AdvancedCustomConfig {
	var setting dto.ChannelSettings
	if ch.Type != constant.ChannelTypeTaskPlugin || ch.Setting == nil || common.UnmarshalJsonStr(*ch.Setting, &setting) != nil {
		return nil
	}
	plugin, ok := jsplugin.DefaultRegistry.Get(setting.TaskPluginKey)
	if !ok {
		return nil
	}
	return plugin.Meta.RelayConfig()
}

// UpdateScheduledChannelLocked persists a completed scheduled result. update
// only compares the prepared key and applies validated data to the current row;
// it must not run JavaScript or perform network I/O. The key lock and the
// database transaction therefore cover only the short persistence step.
func UpdateScheduledChannelLocked(ctx context.Context, channelID int, update func(channel *Channel) error) (*Channel, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		keyLock := sqliteChannelKeyLock(channelID)
		keyLock.Lock()
		defer keyLock.Unlock()
	}
	var channel Channel
	var keyChanged, statusChanged, otherChanged, written bool
	// Failed SQL must not include a credential, including in debug mode.
	db := DB.WithContext(ctx).Session(&gorm.Session{Logger: logger.Discard})
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("id = ?", channelID).First(&channel).Error; err != nil {
			return err
		}
		key, status, otherInfo := channel.Key, channel.Status, channel.OtherInfo
		if err := update(&channel); err != nil {
			return err
		}
		updates := map[string]any{}
		if channel.Key != key {
			updates["key"], keyChanged = channel.Key, true
		}
		if channel.Status != status {
			updates["status"], statusChanged = channel.Status, true
		}
		if channel.OtherInfo != otherInfo {
			updates["other_info"], otherChanged = channel.OtherInfo, true
		}
		if len(updates) == 0 {
			return nil
		}
		written = true
		if err := tx.Model(&Channel{}).Where("id = ?", channelID).Updates(updates).Error; err != nil {
			return err
		}
		if statusChanged {
			return writeAbilityStatus(tx, channelID, channel.Status)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !written {
		return &channel, nil
	}
	defer BumpSyncTopic(SyncTopicChannels)
	if keyChanged {
		CacheUpdateChannelKey(channelID, channel.Key)
	}
	if common.MemoryCacheEnabled && (statusChanged || otherChanged) {
		channelSyncLock.Lock()
		if cached, ok := channelsIDM[channelID]; ok {
			updated := *cached
			updated.Key = channel.Key
			updated.OtherInfo = channel.OtherInfo
			channelsIDM[channelID] = &updated
		}
		channelSyncLock.Unlock()
		CacheUpdateChannelStatus(channelID, channel.Status)

	}
	if err := publishChannelRoutingIDs([]int{channelID}); err != nil {
		return nil, err
	}
	return &channel, nil
}

// SetScheduledChannelStatus applies an automatic status transition to the
// channel. Call it only inside UpdateScheduledChannelLocked, after checking
// the key again; a manual disable can never be undone by a plugin.
func SetScheduledChannelStatus(channel *Channel, status int, reason string) bool {
	switch status {
	case common.ChannelStatusEnabled:
		if channel.Status != common.ChannelStatusAutoDisabled {
			return false
		}
	case common.ChannelStatusAutoDisabled:
		if channel.Status != common.ChannelStatusEnabled || !channel.GetAutoBan() {
			return false
		}
	default:
		return false
	}
	channel.Status = status
	info := channel.GetOtherInfo()
	info["status_reason"] = reason
	info["status_time"] = common.GetTimestamp()
	channel.SetOtherInfo(info)
	return true
}
