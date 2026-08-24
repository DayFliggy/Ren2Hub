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
var ErrCapabilitySnapshotNotFound = errors.New("channel capability snapshot not found")

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

// FindActiveChannelCapabilities returns only rows fenced by the channel's
// active snapshot pointer. Historical rows are retained for replay, but must
// never leak into request-time catalog or candidate discovery queries.
func FindActiveChannelCapabilities(ctx context.Context, channelIDs []int, requestModel, lab string) ([]ChannelModelCapability, error) {
	if DB == nil {
		return nil, errors.New("capability snapshot database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(channelIDs) == 0 {
		return []ChannelModelCapability{}, nil
	}
	query := DB.WithContext(ctx).Model(&ChannelModelCapability{}).
		Joins("JOIN channel_capability_snapshots AS capability_snapshots ON capability_snapshots.channel_id = channel_model_capabilities.channel_id AND capability_snapshots.active_version = channel_model_capabilities.snapshot_version").
		Where("channel_model_capabilities.channel_id IN ? AND capability_snapshots.active_version > 0", channelIDs)
	if requestModel != "" {
		query = query.Where("channel_model_capabilities.request_model = ?", requestModel)
	}
	if lab != "" {
		query = query.Where("channel_model_capabilities.lab_slug = ?", lab)
	}
	var capabilities []ChannelModelCapability
	if err := query.Order("channel_model_capabilities.request_model asc, channel_model_capabilities.channel_id asc").Find(&capabilities).Error; err != nil {
		return nil, err
	}
	return capabilities, nil
}

// FindChannelCapabilitySnapshotVersion loads an immutable historical snapshot
// for diagnostics or deterministic route-decision replay. It intentionally
// does not consult the active pointer and must not be used for live routing.
func FindChannelCapabilitySnapshotVersion(ctx context.Context, channelID int, snapshotVersion int64) ([]ChannelModelCapability, error) {
	if DB == nil {
		return nil, errors.New("capability snapshot database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if channelID <= 0 || snapshotVersion <= 0 {
		return nil, ErrCapabilitySnapshotNotFound
	}
	var capabilities []ChannelModelCapability
	if err := DB.WithContext(ctx).Where("channel_id = ? AND snapshot_version = ?", channelID, snapshotVersion).
		Order("request_model asc, id asc").Find(&capabilities).Error; err != nil {
		return nil, err
	}
	if len(capabilities) == 0 {
		return nil, ErrCapabilitySnapshotNotFound
	}
	return capabilities, nil
}
