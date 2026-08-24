package model

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

var ErrCapabilitySnapshotConflict = errors.New("channel capability snapshot version conflict")

var capabilityRefreshHook struct {
	sync.RWMutex
	fn func(channelID int)
}

func SetChannelCapabilityRefreshHook(fn func(channelID int)) {
	capabilityRefreshHook.Lock()
	capabilityRefreshHook.fn = fn
	capabilityRefreshHook.Unlock()
}

func NotifyChannelCapabilityChanged(channelID int) {
	capabilityRefreshHook.RLock()
	fn := capabilityRefreshHook.fn
	capabilityRefreshHook.RUnlock()
	if fn != nil {
		fn(channelID)
	}
}

// PublishChannelCapabilitySnapshot atomically writes a new immutable channel
// snapshot and fences it into the active pointer. The caller owns capability
// projection; this function owns transaction, CAS, retention, and rollback.
func PublishChannelCapabilitySnapshot(ctx context.Context, channelID int, sourceHash, catalogVersion string, capabilities []ChannelModelCapability) error {
	if DB == nil || channelID <= 0 {
		return errors.New("capability snapshot database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current ChannelCapabilitySnapshot
		err := lockForUpdate(tx).Where("channel_id = ?", channelID).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			current = ChannelCapabilitySnapshot{
				ChannelID:     channelID,
				ActiveVersion: 0,
				RefreshStatus: RouteCapabilityRefreshBuilding,
			}
			if err := tx.Create(&current).Error; err != nil {
				return ErrCapabilitySnapshotConflict
			}
		} else if err != nil {
			return err
		}

		oldVersion := current.ActiveVersion
		nextVersion := oldVersion + 1
		if nextVersion <= 0 {
			return errors.New("capability snapshot version overflow")
		}
		now := time.Now().Unix()
		for index := range capabilities {
			capability := &capabilities[index]
			capability.ChannelID = channelID
			capability.SnapshotVersion = nextVersion
			capability.SourceHash = sourceHash
			capability.CatalogVersion = catalogVersion
			capability.Normalize(time.Unix(now, 0))
		}
		if len(capabilities) > 0 {
			if err := tx.Create(&capabilities).Error; err != nil {
				return err
			}
		}

		result := tx.Model(&ChannelCapabilitySnapshot{}).
			Where("id = ? AND channel_id = ? AND active_version = ?", current.ID, channelID, oldVersion).
			Updates(map[string]any{
				"active_version":  nextVersion,
				"catalog_version": catalogVersion,
				"source_hash":     sourceHash,
				"refresh_status":  RouteCapabilityRefreshActive,
				"refreshed_at":    now,
				"updated_at":      now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCapabilitySnapshotConflict
		}

		// Keep the active version and two predecessors for replay/diagnostics.
		return tx.Where("channel_id = ? AND snapshot_version < ?", channelID, nextVersion-2).
			Delete(&ChannelModelCapability{}).Error
	})
}

// MarkChannelCapabilityRefreshFailure keeps the previous active version
// readable while exposing the latest failed refresh state to operators.
func MarkChannelCapabilityRefreshFailure(channelID int, sourceHash, catalogVersion string) error {
	if DB == nil || channelID <= 0 {
		return errors.New("capability snapshot database is unavailable")
	}
	updates := map[string]any{
		"refresh_status": RouteCapabilityRefreshFailed,
		"updated_at":     common.GetTimestamp(),
	}
	// A refresh can finish after a newer snapshot has already won the CAS. The
	// failure marker must only describe the snapshot that the failed attempt
	// observed; otherwise an old worker can mark the newer active snapshot as
	// failed without changing its version.
	result := DB.Model(&ChannelCapabilitySnapshot{}).
		Where("channel_id = ? AND source_hash = ? AND catalog_version = ?", channelID, sourceHash, catalogVersion).
		Updates(updates)
	return result.Error
}
