package model

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrProviderUsageIdentityChanged = errors.New("provider usage credential or endpoint changed")
var ErrProviderUsageCooldown = errors.New("provider usage refresh is in cooldown")

// ProviderUsageSnapshot is an observation, never routing or billing evidence.
// Only normalized windows are persisted; provider bodies and credentials are not.
type ProviderUsageSnapshot struct {
	ChannelID      int    `gorm:"primaryKey"`
	CredentialID   string `gorm:"type:varchar(36);not null"`
	EndpointID     string `gorm:"type:varchar(64);not null"`
	AttemptID      string `gorm:"type:varchar(36);not null"`
	State          string `gorm:"type:varchar(16);not null"`
	PlanType       string `gorm:"type:varchar(64)"`
	Windows        LongText
	FetchedAt      int64 `gorm:"type:bigint;not null"`
	ExpiresAt      int64 `gorm:"type:bigint;not null"`
	LastAttemptAt  int64 `gorm:"type:bigint;not null"`
	NextRefreshAt  int64 `gorm:"type:bigint;not null"`
	FailureCount   int
	UpstreamStatus int
	ErrorCode      string `gorm:"type:varchar(48)"`
}

type ProviderUsageSetting struct {
	ChannelID int  `gorm:"primaryKey"`
	Enabled   bool `gorm:"not null;index"`
}

func ReadProviderUsageSetting(ctx context.Context, channelID int) (ProviderUsageSetting, error) {
	setting := ProviderUsageSetting{ChannelID: channelID}
	err := DB.WithContext(ctx).Where("channel_id = ?", channelID).First(&setting).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ProviderUsageSetting{ChannelID: channelID}, nil
	}
	return setting, err
}

func SaveProviderUsageSetting(ctx context.Context, channelID int, enabled bool) error {
	return DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "channel_id"}}, DoUpdates: clause.AssignmentColumns([]string{"enabled"})}).
		Create(&ProviderUsageSetting{ChannelID: channelID, Enabled: enabled}).Error
}

// Reading does not create a credential identity or invalidate a row by writing.
// A replacement key/endpoint simply has no observable cached snapshot.
func ReadProviderUsageSnapshot(ctx context.Context, channel *Channel) (*ProviderUsageSnapshot, error) {
	var credential GatewayCredential
	err := DB.WithContext(ctx).Where("channel_id = ? AND fingerprint = ?", channel.Id, GatewayDigest(strings.TrimSpace(channel.Key))).First(&credential).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot ProviderUsageSnapshot
	err = DB.WithContext(ctx).Where("channel_id = ? AND credential_id = ? AND endpoint_id = ?", channel.Id, credential.ID, channel.GatewayEndpointID()).First(&snapshot).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &snapshot, err
}

func checkProviderUsageIdentity(tx *gorm.DB, channelID int, credentialID, endpointID string) error {
	var current Channel
	if err := lockForUpdate(tx).Where("id = ?", channelID).First(&current).Error; err != nil {
		return err
	}
	if current.ChannelInfo.IsMultiKey || current.GatewayEndpointID() != endpointID {
		return ErrProviderUsageIdentityChanged
	}
	var credential GatewayCredential
	if err := tx.Where("id = ? AND channel_id = ?", credentialID, channelID).First(&credential).Error; err != nil {
		return err
	}
	if credential.Fingerprint != GatewayDigest(strings.TrimSpace(current.Key)) {
		return ErrProviderUsageIdentityChanged
	}
	return nil
}

// Persist the cooldown before the network request so a restart cannot erase it.
func BeginProviderUsageRefresh(ctx context.Context, channelID int, credentialID, endpointID string, now, interval int64) (ProviderUsageSnapshot, error) {
	var snapshot ProviderUsageSnapshot
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkProviderUsageIdentity(tx, channelID, credentialID, endpointID); err != nil {
			return err
		}
		err := tx.Where("channel_id = ?", channelID).First(&snapshot).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) || snapshot.CredentialID != credentialID || snapshot.EndpointID != endpointID {
			snapshot = ProviderUsageSnapshot{ChannelID: channelID, CredentialID: credentialID, EndpointID: endpointID, State: "unknown", Windows: "[]"}
		} else if snapshot.NextRefreshAt > now || snapshot.LastAttemptAt+interval > now {
			return ErrProviderUsageCooldown
		}
		snapshot.AttemptID = uuid.NewString()
		snapshot.LastAttemptAt, snapshot.NextRefreshAt = now, now+60
		return tx.Save(&snapshot).Error
	})
	return snapshot, err
}

// A late request cannot overwrite a replacement credential's observation.
// A trusted OAuth refresh retains the stable ID and is intentionally accepted.
func FinishProviderUsageRefresh(ctx context.Context, snapshot ProviderUsageSnapshot) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkProviderUsageIdentity(tx, snapshot.ChannelID, snapshot.CredentialID, snapshot.EndpointID); err != nil {
			return err
		}
		result := tx.Model(&ProviderUsageSnapshot{}).Where("channel_id = ? AND credential_id = ? AND endpoint_id = ? AND attempt_id = ?", snapshot.ChannelID, snapshot.CredentialID, snapshot.EndpointID, snapshot.AttemptID).
			Select("state", "plan_type", "windows", "fetched_at", "expires_at", "next_refresh_at", "failure_count", "upstream_status", "error_code").Updates(&snapshot)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrProviderUsageIdentityChanged
		}
		return nil
	})
}

func ListProviderUsageEnabledChannels(ctx context.Context, afterID, limit int) ([]int, error) {
	var settings []ProviderUsageSetting
	if err := DB.WithContext(ctx).Where("enabled = ? AND channel_id > ?", true, afterID).Order("channel_id").Limit(limit).Find(&settings).Error; err != nil {
		return nil, err
	}
	ids := make([]int, len(settings))
	for i, setting := range settings {
		ids[i] = setting.ChannelID
	}
	return ids, nil
}
