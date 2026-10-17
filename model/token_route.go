package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TokenRouteAuto       = "auto"
	TokenRouteManual     = "manual"
	MaxTokenRouteEntries = 100
)

var (
	ErrTokenRouteInvalid   = errors.New("invalid token routing configuration")
	ErrTokenRouteConflict  = errors.New("token routing version conflict")
	ErrTokenRouteForbidden = errors.New("channel is unavailable or unauthorized")
)

// Keep the automatic row after a reset so stale editors cannot reuse version 0.
type TokenRouteProfile struct {
	TokenID int    `json:"token_id" gorm:"primaryKey;autoIncrement:false"`
	Mode    string `json:"mode" gorm:"type:varchar(16);not null"`
	Version int64  `json:"version" gorm:"not null"`
}

type TokenRouteEntry struct {
	TokenID   int  `json:"-" gorm:"primaryKey;autoIncrement:false"`
	ChannelID int  `json:"channel_id" gorm:"primaryKey;autoIncrement:false"`
	Position  int  `json:"position" gorm:"not null"`
	Enabled   bool `json:"enabled" gorm:"not null"`
}

type TokenRouting struct {
	TokenRouteProfile
	Entries []TokenRouteEntry `json:"entries"`
}

func ReadTokenRouting(ctx context.Context, tokenID int) (TokenRouting, error) {
	return readTokenRouting(DB.WithContext(ctx), tokenID)
}

func readTokenRouting(db *gorm.DB, tokenID int) (TokenRouting, error) {
	view := TokenRouting{TokenRouteProfile: TokenRouteProfile{TokenID: tokenID, Mode: TokenRouteAuto}, Entries: []TokenRouteEntry{}}
	result := db.Where("token_id = ?", tokenID).Find(&view.TokenRouteProfile)
	if result.Error != nil {
		return view, result.Error
	}
	if result.RowsAffected == 0 {
		return view, nil
	}
	if view.Mode != TokenRouteAuto && view.Mode != TokenRouteManual {
		return view, ErrTokenRouteInvalid
	}
	err := db.Where("token_id = ?", tokenID).Order("position asc, channel_id asc").Find(&view.Entries).Error
	return view, err
}

func ValidateTokenRouting(input TokenRouting) error {
	if input.Mode != TokenRouteAuto && input.Mode != TokenRouteManual || input.Version < 0 || len(input.Entries) > MaxTokenRouteEntries {
		return ErrTokenRouteInvalid
	}
	if input.Mode == TokenRouteAuto && len(input.Entries) != 0 {
		return ErrTokenRouteInvalid
	}
	seen := make(map[int]bool, len(input.Entries))
	for _, entry := range input.Entries {
		if entry.ChannelID <= 0 || seen[entry.ChannelID] {
			return ErrTokenRouteInvalid
		}
		seen[entry.ChannelID] = true
	}
	return nil
}

// Ownership and the revision update share a transaction. Candidate permissions
// are also checked on every relay attempt, independently of this saved preference.
func SaveTokenRouting(ctx context.Context, userID, tokenID int, input TokenRouting, allowed map[int]bool) (TokenRouting, error) {
	if err := ValidateTokenRouting(input); err != nil {
		return TokenRouting{}, err
	}
	var result TokenRouting
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var token Token
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
			return err
		}
		current, err := readTokenRouting(tx, tokenID)
		if err != nil {
			return err
		}
		if current.Version != input.Version {
			return ErrTokenRouteConflict
		}
		existing := make(map[int]bool, len(current.Entries))
		for _, entry := range current.Entries {
			existing[entry.ChannelID] = true
		}
		for _, entry := range input.Entries {
			if !allowed[entry.ChannelID] && !existing[entry.ChannelID] {
				return ErrTokenRouteForbidden
			}
		}
		profile := TokenRouteProfile{TokenID: tokenID, Mode: input.Mode, Version: current.Version + 1}
		if current.Version == 0 {
			if err := tx.Create(&profile).Error; err != nil {
				if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
					return ErrTokenRouteConflict
				}
				return err
			}
		} else {
			update := tx.Model(&TokenRouteProfile{}).Where("token_id = ? AND version = ?", tokenID, current.Version).Updates(map[string]any{"mode": profile.Mode, "version": profile.Version})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return ErrTokenRouteConflict
			}
		}
		if err := tx.Where("token_id = ?", tokenID).Delete(&TokenRouteEntry{}).Error; err != nil {
			return err
		}
		entries := make([]TokenRouteEntry, len(input.Entries))
		for index, entry := range input.Entries {
			entries[index] = TokenRouteEntry{TokenID: tokenID, ChannelID: entry.ChannelID, Position: index, Enabled: entry.Enabled}
		}
		if len(entries) > 0 {
			if err := tx.Create(&entries).Error; err != nil {
				return err
			}
		}
		result = TokenRouting{TokenRouteProfile: profile, Entries: entries}
		return nil
	})
	return result, err
}

func (token *Token) InsertWithRouting(mode string) error {
	if mode == "" || mode == TokenRouteAuto {
		return token.Insert()
	}
	if mode != TokenRouteManual {
		return ErrTokenRouteInvalid
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(token).Error; err != nil {
			return err
		}
		return tx.Create(&TokenRouteProfile{TokenID: token.Id, Mode: mode, Version: 1}).Error
	})
}

// TokenRoutingModes is used only by dashboard views, never by key validation.
func TokenRoutingModes(tokenIDs []int) (map[int]string, error) {
	modes := make(map[int]string, len(tokenIDs))
	if len(tokenIDs) == 0 {
		return modes, nil
	}
	var profiles []TokenRouteProfile
	if err := DB.Where("token_id IN ?", tokenIDs).Find(&profiles).Error; err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.Mode != TokenRouteAuto && profile.Mode != TokenRouteManual {
			return nil, fmt.Errorf("%w: token %d", ErrTokenRouteInvalid, profile.TokenID)
		}
		modes[profile.TokenID] = profile.Mode
	}
	return modes, nil
}

func deleteTokenRouting(tx *gorm.DB, tokenIDs []int) error {
	if len(tokenIDs) == 0 {
		return nil
	}
	if err := tx.Where("token_id IN ?", tokenIDs).Delete(&TokenRouteEntry{}).Error; err != nil {
		return err
	}
	return tx.Where("token_id IN ?", tokenIDs).Delete(&TokenRouteProfile{}).Error
}

// PublicRouteChannels projects enabled abilities, without loading credentials.
func PublicRouteChannels(ctx context.Context, groups []string) ([]Channel, []Ability, error) {
	channels, abilities := []Channel{}, []Ability{}
	if len(groups) == 0 {
		return channels, abilities, nil
	}
	if err := DB.WithContext(ctx).Where(map[string]any{"group": groups, "enabled": true}).Find(&abilities).Error; err != nil {
		return nil, nil, err
	}
	ids := make([]int, 0, len(abilities))
	for _, ability := range abilities {
		ids = append(ids, ability.ChannelId)
	}
	if len(ids) == 0 {
		return channels, abilities, nil
	}
	err := DB.WithContext(ctx).Select("id", "name", "type", "status").Where("id IN ? AND status = ?", ids, common.ChannelStatusEnabled).Order("id asc").Find(&channels).Error
	return channels, abilities, err
}
