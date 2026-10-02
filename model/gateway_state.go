package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrGatewayStateUnknown = errors.New("state is unknown or unavailable to this token; start a new conversation")
var ErrGatewayStateUnavailable = errors.New("state owner is temporarily unavailable")

type GatewayCredential struct {
	ID          string `gorm:"type:varchar(36);primaryKey" json:"id"`
	ChannelID   int    `gorm:"uniqueIndex:gateway_credential_lookup" json:"-"`
	Fingerprint string `gorm:"type:varchar(64);uniqueIndex:gateway_credential_lookup" json:"-"`
	CreatedAt   int64  `json:"-"`
}

type GatewayStateBinding struct {
	ID           uint64 `gorm:"primaryKey"`
	UserID       int    `gorm:"uniqueIndex:gateway_state_lookup"`
	TokenID      int    `gorm:"uniqueIndex:gateway_state_lookup"`
	Kind         string `gorm:"type:varchar(32);uniqueIndex:gateway_state_lookup"`
	Digest       string `gorm:"type:varchar(64);uniqueIndex:gateway_state_lookup"`
	ChannelID    int    `gorm:"index"`
	CredentialID string `gorm:"type:varchar(36)"`
	EndpointID   string `gorm:"type:varchar(64)"`
	TransportID  string `gorm:"type:varchar(64)"`
	CreatedAt    int64
	ExpiresAt    int64 `gorm:"index"`
}

func GatewayDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (channel *Channel) GatewayEndpointID() string {
	organization := ""
	if channel.OpenAIOrganization != nil {
		organization = *channel.OpenAIOrganization
	}
	return GatewayDigest(fmt.Sprintf("%d\n%s\n%s\n%s\n%d", channel.Type, strings.TrimRight(channel.GetBaseURL(), "/"), organization, channel.Other, channel.GatewayIdentityVersion))
}

func (channel *Channel) gatewayCredentialSet() []string {
	keys := []string{channel.Key}
	if channel.ChannelInfo.IsMultiKey {
		keys = channel.GetKeys()
	}
	digests := make([]string, 0, len(keys))
	for _, key := range keys {
		digests = append(digests, GatewayDigest(strings.TrimSpace(key)))
	}
	slices.Sort(digests)
	return slices.Compact(digests)
}

func EnsureGatewayCredential(ctx context.Context, channelID int, key string) (GatewayCredential, error) {
	credential := GatewayCredential{ID: uuid.NewString(), ChannelID: channelID, Fingerprint: GatewayDigest(strings.TrimSpace(key)), CreatedAt: time.Now().Unix()}
	if channelID <= 0 || strings.TrimSpace(key) == "" {
		return credential, ErrGatewayStateUnavailable
	}
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&credential).Error; err != nil {
		return credential, err
	}
	credential.ID = ""
	err := DB.WithContext(ctx).Where("channel_id = ? AND fingerprint = ?", channelID, credential.Fingerprint).First(&credential).Error
	return credential, err
}

func ResolveGatewayCredential(ctx context.Context, channel *Channel, identity string) (string, int, error) {
	var credential GatewayCredential
	if err := DB.WithContext(ctx).Where("id = ? AND channel_id = ?", identity, channel.Id).First(&credential).Error; err != nil {
		return "", 0, ErrGatewayStateUnavailable
	}
	return channel.enabledGatewayKey(credential.Fingerprint)
}

func (channel *Channel) HasEnabledGatewayKey(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	_, _, err := channel.enabledGatewayKey(GatewayDigest(strings.TrimSpace(key)))
	return err == nil
}

func (channel *Channel) enabledGatewayKey(fingerprint string) (string, int, error) {
	keys := channel.GetKeys()
	if !channel.ChannelInfo.IsMultiKey {
		keys = []string{channel.Key}
	}
	for index, key := range keys {
		if status, set := channel.ChannelInfo.MultiKeyStatusList[index]; set && status != common.ChannelStatusEnabled {
			continue
		}
		if GatewayDigest(strings.TrimSpace(key)) == fingerprint {
			return key, index, nil
		}
	}
	return "", 0, ErrGatewayStateUnavailable
}

// RefreshGatewayCredential preserves identity only through the trusted refresh path.
func RefreshGatewayCredential(ctx context.Context, channel *Channel, replacement string) error {
	identity, err := EnsureGatewayCredential(ctx, channel.Id, channel.Key)
	if err != nil {
		return err
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&Channel{}).Where(map[string]any{"id": channel.Id, "key": channel.Key}).Update("key", replacement)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("credential changed during refresh")
		}
		return tx.Model(&GatewayCredential{}).Where("id = ? AND fingerprint = ?", identity.ID, identity.Fingerprint).Update("fingerprint", GatewayDigest(strings.TrimSpace(replacement))).Error
	})
}

func FindGatewayState(ctx context.Context, userID, tokenID int, kind, value string) (GatewayStateBinding, error) {
	var binding GatewayStateBinding
	err := DB.WithContext(ctx).Where("user_id = ? AND token_id = ? AND kind = ? AND digest = ? AND expires_at > ?", userID, tokenID, kind, GatewayDigest(value), time.Now().Unix()).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return binding, ErrGatewayStateUnknown
	}
	return binding, err
}

func BindGatewayState(ctx context.Context, binding GatewayStateBinding, value string) error {
	if binding.UserID <= 0 || binding.TokenID <= 0 || binding.CredentialID == "" || value == "" {
		return ErrGatewayStateUnknown
	}
	binding.Digest, binding.CreatedAt = GatewayDigest(value), time.Now().Unix()
	maximumExpiry := time.Now().Add(30 * 24 * time.Hour).Unix()
	if binding.ExpiresAt == 0 || binding.ExpiresAt > maximumExpiry {
		binding.ExpiresAt = maximumExpiry
	}
	if binding.ExpiresAt <= binding.CreatedAt {
		return ErrGatewayStateUnknown
	}
	if err := DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&binding).Error; err != nil {
		return err
	}
	stored, err := FindGatewayState(ctx, binding.UserID, binding.TokenID, binding.Kind, value)
	if err != nil {
		return err
	}
	if !stored.SameOwner(binding) {
		return ErrGatewayStateUnknown
	}
	// A later event may report a shorter provider lifetime, but replaying the
	// same identifier must never extend its original retention window.
	return DB.WithContext(ctx).Model(&GatewayStateBinding{}).
		Where("id = ? AND expires_at > ?", stored.ID, binding.ExpiresAt).
		Update("expires_at", binding.ExpiresAt).Error
}

func (binding GatewayStateBinding) SameOwner(other GatewayStateBinding) bool {
	return binding.ChannelID == other.ChannelID && binding.CredentialID == other.CredentialID &&
		binding.EndpointID == other.EndpointID && binding.TransportID == other.TransportID
}

func DeleteExpiredGatewayStates(ctx context.Context, cutoff int64, limit int) (int64, error) {
	if limit <= 0 || limit > 1000 {
		return 0, errors.New("state cleanup batch must contain 1 to 1000 records")
	}
	var ids []uint64
	if err := DB.WithContext(ctx).Model(&GatewayStateBinding{}).
		Where("expires_at <= ?", cutoff).Order("expires_at, id").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.WithContext(ctx).Where("id IN ? AND expires_at <= ?", ids, cutoff).Delete(&GatewayStateBinding{})
	return result.RowsAffected, result.Error
}
