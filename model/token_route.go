package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TokenRouteAuto       = "auto"
	TokenRouteOrdered    = "ordered"
	MaxTokenRouteEntries = 100
)

var (
	ErrTokenRouteInvalid   = errors.New("invalid token routing configuration")
	ErrTokenRouteConflict  = errors.New("token routing version conflict")
	ErrTokenRouteForbidden = errors.New("channel is unavailable or unauthorized")
)

// Keep the automatic row after a reset so stale editors cannot reuse version 0.
type TokenRouteProfile struct {
	TokenID         int     `json:"token_id" gorm:"primaryKey;autoIncrement:false"`
	Mode            string  `json:"mode" gorm:"type:varchar(16);not null"`
	SelectedOnly    bool    `json:"selected_only" gorm:"not null;default:false"`
	MaxChannelRatio *string `json:"max_channel_ratio" gorm:"type:varchar(64)"`
	Version         int64   `json:"version" gorm:"not null"`
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
	if view.Mode != TokenRouteAuto && view.Mode != TokenRouteOrdered {
		return view, ErrTokenRouteInvalid
	}
	err := db.Where("token_id = ?", tokenID).Order("position asc, channel_id asc").Find(&view.Entries).Error
	return view, err
}

func ValidateTokenRouting(input TokenRouting) error {
	if input.MaxChannelRatio != nil {
		if _, err := NormalizeChannelRatio(*input.MaxChannelRatio); err != nil {
			return ErrTokenRouteInvalid
		}
	}
	if input.Mode != TokenRouteAuto && input.Mode != TokenRouteOrdered || input.Version < 0 || len(input.Entries) > MaxTokenRouteEntries {
		return ErrTokenRouteInvalid
	}
	if input.Mode == TokenRouteOrdered && !input.SelectedOnly || input.SelectedOnly != (len(input.Entries) > 0) {
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
// Lock order is the token row, then the referenced channel rows in ascending ID
// order, then route rows; Token.Delete and BatchDeleteTokens take the token row first too.
func SaveTokenRouting(ctx context.Context, userID, tokenID int, input TokenRouting, allowed map[int]bool) (TokenRouting, error) {
	if err := ValidateTokenRouting(input); err != nil {
		return TokenRouting{}, err
	}
	if input.MaxChannelRatio != nil {
		normalized, err := NormalizeChannelRatio(*input.MaxChannelRatio)
		if err != nil {
			return TokenRouting{}, err
		}
		input.MaxChannelRatio = &normalized
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
		if err := lockRouteChannels(tx, input.Entries); err != nil {
			return err
		}
		profile := TokenRouteProfile{TokenID: tokenID, Mode: input.Mode, SelectedOnly: input.SelectedOnly, MaxChannelRatio: input.MaxChannelRatio, Version: current.Version + 1}
		if current.Version == 0 {
			if err := tx.Create(&profile).Error; err != nil {
				if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
					return ErrTokenRouteConflict
				}
				return err
			}
		} else {
			update := tx.Model(&TokenRouteProfile{}).Where("token_id = ? AND version = ?", tokenID, current.Version).Updates(map[string]any{"mode": profile.Mode, "selected_only": profile.SelectedOnly, "max_channel_ratio": profile.MaxChannelRatio, "version": profile.Version})
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

// lockRouteChannels 在保存事务里对最终输入的全部渠道（新增和已有）做锁定读。删除渠道要等
// 本事务结束，之后的孤儿清理能看到这里写入的条目；渠道行已被删除时返回 ErrTokenRouteForbidden，
// 由用户在编辑器里删掉该条目，不静默丢弃也不插回。只确认渠道行仍在，不看启用和倍率，
// 已保存但暂时不可用的条目仍可编辑。共享锁足以挡住删除，也不与孤儿清理在 MySQL 上读渠道时
// 加的共享锁互斥。SQLite 没有锁定读，GORM 不生成该子句；SQLite 的写事务本身串行。
func lockRouteChannels(tx *gorm.DB, entries []TokenRouteEntry) error {
	if len(entries) == 0 {
		return nil
	}
	ids := make([]int, len(entries))
	for index, entry := range entries {
		ids[index] = entry.ChannelID
	}
	found, err := lockChannels(tx, ids, clause.LockingStrengthShare)
	if err != nil {
		return err
	}
	if len(found) != len(ids) {
		return ErrTokenRouteForbidden
	}
	return nil
}

type TokenRoutingSummary struct {
	TokenID           int
	Mode              string
	SelectedOnly      bool
	RouteChannelCount int
}

// TokenRoutingModes reads mode, scope and count in one statement so an editor
// saving a new version cannot mix old settings with the new entry count.
func TokenRoutingModes(tokenIDs []int) (map[int]TokenRoutingSummary, error) {
	modes := make(map[int]TokenRoutingSummary, len(tokenIDs))
	if len(tokenIDs) == 0 {
		return modes, nil
	}
	var profiles []TokenRoutingSummary
	count := DB.Model(&TokenRouteEntry{}).Select("count(*)").Where("token_id = profiles.token_id")
	err := DB.Model(&TokenRouteProfile{}).Table("(?) AS profiles", DB.Model(&TokenRouteProfile{}).Select("token_id, mode, selected_only")).Select("profiles.*, (?) AS route_channel_count", count).Where("profiles.token_id IN ?", tokenIDs).Scan(&profiles).Error
	if err != nil {
		return nil, err
	}
	for _, profile := range profiles {
		if profile.Mode != TokenRouteAuto && profile.Mode != TokenRouteOrdered {
			return nil, fmt.Errorf("%w: token %d", ErrTokenRouteInvalid, profile.TokenID)
		}
		modes[profile.TokenID] = profile
	}
	return modes, nil
}

// DeleteOrphanChannelRouteEntries 删除指向已不存在渠道的 Key 路由条目。
// 渠道没有软删除，按“渠道已不存在”清扫而不是按本次 id，失败或并发写入留下的残留
// 会在下一次删除渠道时一并清掉。
func DeleteOrphanChannelRouteEntries(ctx context.Context) error {
	existing := DB.Model(&Channel{}).Select("id")
	return DB.WithContext(ctx).Where("channel_id NOT IN (?)", existing).Delete(&TokenRouteEntry{}).Error
}

// deleteUserTokenRouting 在硬删除用户的事务里清理该用户全部令牌（含已软删除的）的
// Key 路由，规则与删除单个令牌共用 deleteTokenRouting。令牌行按 ID 升序先锁住，锁序与
// SaveTokenRouting、Token.Delete 相同：保存不能插在清理路由和删除令牌之间写入。
func deleteUserTokenRouting(tx *gorm.DB, userID int) error {
	var tokenIDs []int
	if err := lockForUpdate(tx.Unscoped().Model(&Token{})).Where("user_id = ?", userID).Order("id").Pluck("id", &tokenIDs).Error; err != nil {
		return err
	}
	for _, chunk := range lo.Chunk(tokenIDs, 200) {
		if err := deleteTokenRouting(tx, chunk); err != nil {
			return err
		}
	}
	return nil
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
