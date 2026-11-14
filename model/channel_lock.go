package model

import (
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// lockChannels sorts the complete set before chunking. Every multi-channel
// writer uses UPDATE in this order; route saves use the same order with SHARE.
// The returned rows are a current locking read, including on MySQL, so callers
// can recheck a tag/status predicate after a concurrent edit or deletion.
func lockChannels(tx *gorm.DB, ids []int, strength string) ([]Channel, error) {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var channels []Channel
	for chunk := range slices.Chunk(ids, 200) {
		var rows []Channel
		if err := tx.Session(&gorm.Session{}).Clauses(clause.Locking{Strength: strength}).Where("id IN ?", chunk).Order("id").Find(&rows).Error; err != nil {
			return nil, err
		}
		channels = append(channels, rows...)
	}
	return channels, nil
}

func lockedChannelIDs(channels []Channel) []int {
	ids := make([]int, len(channels))
	for index := range channels {
		ids[index] = channels[index].Id
	}
	return ids
}

func lockChannelsByTag(tx *gorm.DB, tag string) ([]Channel, error) {
	var ids []int
	if err := tx.Model(&Channel{}).Where("tag = ?", tag).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	channels, err := lockChannels(tx, ids, clause.LockingStrengthUpdate)
	if err != nil {
		return nil, err
	}
	// Recheck with the database's predicate/collation, not Go string equality.
	// These IDs are already locked; a current read also avoids MySQL's earlier
	// candidate snapshot and excludes rows that entered the tag afterwards.
	return lockChannels(tx.Where("tag = ?", tag), lockedChannelIDs(channels), clause.LockingStrengthUpdate)
}

// updateLockedChannels writes only rows already locked and predicate-checked.
// Reload through tx with a current read: a global DB read could wait on our
// locks or return old values; a new-tag query could include unrelated channels.
func updateLockedChannels(tx *gorm.DB, channels []Channel, values any, syncAbilities func(*Channel, *gorm.DB) error) error {
	ids := lockedChannelIDs(channels)
	for chunk := range slices.Chunk(ids, 200) {
		if err := tx.Model(&Channel{}).Where("id IN ?", chunk).Updates(values).Error; err != nil {
			return err
		}
	}
	updated, err := lockChannels(tx, ids, clause.LockingStrengthUpdate)
	if err != nil {
		return err
	}
	for index := range updated {
		if err := syncAbilities(&updated[index], tx); err != nil {
			return err
		}
	}
	return nil
}

func updateChannelsByTag(tag string, values any, syncAbilities func(*Channel, *gorm.DB) error) error {
	var ids []int
	err := DB.Transaction(func(tx *gorm.DB) error {
		channels, err := lockChannelsByTag(tx, tag)
		if err != nil {
			return err
		}
		ids = lockedChannelIDs(channels)
		return updateLockedChannels(tx, channels, values, syncAbilities)
	})
	if err != nil {
		return err
	}
	BumpSyncTopic(SyncTopicChannels)
	return publishChannelRoutingIDs(ids)
}
