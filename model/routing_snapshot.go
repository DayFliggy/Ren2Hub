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

// ChannelCapabilitySnapshotFence identifies the active snapshot observed by a
// refresher before it projects a channel. The complete tuple prevents an old
// worker from publishing or recording failure details against a replacement
// with coincidentally compatible version state.
type ChannelCapabilitySnapshotFence struct {
	ActiveVersion  int64
	SourceHash     string
	CatalogVersion string
}

func (f ChannelCapabilitySnapshotFence) matches(snapshot ChannelCapabilitySnapshot) bool {
	return f.ActiveVersion == snapshot.ActiveVersion &&
		f.SourceHash == snapshot.SourceHash &&
		f.CatalogVersion == snapshot.CatalogVersion
}

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
// snapshot and fences it into the active pointer. expected identifies the
// active row read before projection, so a stale worker fails before inserting
// rows for a newer snapshot.
func PublishChannelCapabilitySnapshot(ctx context.Context, channelID int, expected ChannelCapabilitySnapshotFence, sourceHash, catalogVersion string, capabilities []ChannelModelCapability) error {
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
			if expected != (ChannelCapabilitySnapshotFence{}) {
				return ErrCapabilitySnapshotConflict
			}
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

		if !expected.matches(current) {
			return ErrCapabilitySnapshotConflict
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
			Where("id = ? AND channel_id = ? AND active_version = ? AND source_hash = ? AND catalog_version = ?",
				current.ID, channelID, oldVersion, expected.SourceHash, expected.CatalogVersion).
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
// readable while exposing a failed refresh only when the worker still owns the
// active snapshot tuple it observed.
func MarkChannelCapabilityRefreshFailure(channelID int, expected ChannelCapabilitySnapshotFence, sourceHash, catalogVersion string) error {
	if DB == nil || channelID <= 0 {
		return errors.New("capability snapshot database is unavailable")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var current ChannelCapabilitySnapshot
		err := lockForUpdate(tx).Where("channel_id = ?", channelID).First(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expected != (ChannelCapabilitySnapshotFence{}) {
				return ErrCapabilitySnapshotConflict
			}
			current = ChannelCapabilitySnapshot{
				ChannelID:     channelID,
				RefreshStatus: RouteCapabilityRefreshBuilding,
			}
			if err := tx.Create(&current).Error; err != nil {
				return ErrCapabilitySnapshotConflict
			}
		} else if err != nil {
			return err
		}
		if !expected.matches(current) {
			return ErrCapabilitySnapshotConflict
		}
		updates := map[string]any{
			"last_failed_source_hash":     sourceHash,
			"last_failed_catalog_version": catalogVersion,
			"last_failed_at":              common.GetTimestamp(),
			"updated_at":                  common.GetTimestamp(),
		}
		if expected.ActiveVersion <= 0 {
			// A first build has no readable capability rows. Expose its failure so
			// callers can distinguish it from a channel that has never refreshed.
			updates["refresh_status"] = RouteCapabilityRefreshFailed
		}
		// A refresh can finish after a newer snapshot has already won the CAS. The
		// failure marker must only describe the snapshot that the failed attempt
		// observed; otherwise an old worker can mark a replacement snapshot failed.
		result := tx.Model(&ChannelCapabilitySnapshot{}).
			Where("id = ? AND channel_id = ? AND active_version = ? AND source_hash = ? AND catalog_version = ?",
				current.ID, channelID, expected.ActiveVersion, expected.SourceHash, expected.CatalogVersion).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrCapabilitySnapshotConflict
		}
		return nil
	})
}

// GetChannelCapabilitySnapshotFence returns the active tuple a refresher must
// present when publishing or recording a failure. A channel without snapshots
// returns the zero fence.
func GetChannelCapabilitySnapshotFence(ctx context.Context, channelID int) (ChannelCapabilitySnapshotFence, error) {
	if DB == nil {
		return ChannelCapabilitySnapshotFence{}, errors.New("capability snapshot database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var snapshot ChannelCapabilitySnapshot
	err := DB.WithContext(ctx).Where("channel_id = ?", channelID).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ChannelCapabilitySnapshotFence{}, nil
	}
	if err != nil {
		return ChannelCapabilitySnapshotFence{}, err
	}
	return ChannelCapabilitySnapshotFence{
		ActiveVersion:  snapshot.ActiveVersion,
		SourceHash:     snapshot.SourceHash,
		CatalogVersion: snapshot.CatalogVersion,
	}, nil
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
		Where("channel_model_capabilities.channel_id IN ? AND capability_snapshots.active_version > 0 AND capability_snapshots.refresh_status = ?", channelIDs, RouteCapabilityRefreshActive)
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

// FindActiveChannelCapabilitySnapshots returns only channels with a usable
// active pointer. The pointer row is separate from immutable capability rows;
// callers must not infer the active version by scanning historical data.
func FindActiveChannelCapabilitySnapshots(ctx context.Context, channelIDs []int) ([]ChannelCapabilitySnapshot, error) {
	if DB == nil {
		return nil, errors.New("capability snapshot database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if len(channelIDs) == 0 {
		return []ChannelCapabilitySnapshot{}, nil
	}
	var snapshots []ChannelCapabilitySnapshot
	if err := DB.WithContext(ctx).Where("channel_id IN ? AND active_version > 0 AND refresh_status = ?", channelIDs, RouteCapabilityRefreshActive).
		Order("channel_id asc").Find(&snapshots).Error; err != nil {
		return nil, err
	}
	return snapshots, nil
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
