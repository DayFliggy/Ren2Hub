package model

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type UpstreamChannelBinding struct {
	ID                 int    `json:"id" gorm:"primaryKey"`
	AccountID          int    `json:"account_id" gorm:"not null;uniqueIndex:,composite:remote_key,priority:1"`
	RemoteKeyID        int    `json:"remote_key_id" gorm:"not null;uniqueIndex:,composite:remote_key,priority:2"`
	ChannelID          int    `json:"channel_id" gorm:"not null;uniqueIndex"`
	ChannelCreatedTime int64  `json:"channel_created_time" gorm:"not null"`
	ChannelFP          string `json:"-" gorm:"type:varchar(64);not null"`
	Stale              bool   `json:"stale" gorm:"not null;default:false"`
	RemoteStatus       string `json:"remote_status" gorm:"size:32"`
	RatioSyncEnabled   bool   `json:"ratio_sync_enabled" gorm:"not null;default:false"`
	RatioSyncFactor    string `json:"ratio_sync_factor" gorm:"size:64;not null;default:'1'"`
	RatioSyncError     string `json:"ratio_sync_error" gorm:"size:96"`
}

var ErrRemoteKeyAlreadyBound = errors.New("remote_key_already_bound")

func ListUpstreamBindings(accountID int) ([]UpstreamChannelBinding, error) {
	var rows []UpstreamChannelBinding
	err := DB.Where("account_id = ?", accountID).Order("id").Find(&rows).Error
	return rows, err
}

func ListUpstreamBindingsForChannels(ids []int) ([]UpstreamChannelBinding, error) {
	var rows []UpstreamChannelBinding
	current := DB.Model(&Channel{}).Select("id, COALESCE(created_time, 0)").Where("id IN ?", ids)
	err := DB.Where("(channel_id, channel_created_time) IN (?)", current).Order("channel_id").Find(&rows).Error
	return rows, err
}

func UpdateUpstreamBinding(id int, updates map[string]any) error {
	result := DB.Model(&UpstreamChannelBinding{}).Where("id = ?", id).Select("remote_status", "stale", "ratio_sync_enabled", "ratio_sync_factor", "ratio_sync_error").Updates(updates)
	return result.Error
}

func DeleteUpstreamBinding(accountID, channelID int) error {
	result := DB.Where("account_id = ? AND channel_id = ?", accountID, channelID).Delete(&UpstreamChannelBinding{})
	if result.Error == nil && result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return result.Error
}

func insertUpstreamBinding(tx *gorm.DB, b *UpstreamChannelBinding) error {
	current := tx.Model(&Channel{}).Select("id, COALESCE(created_time, 0)")
	matching := func() *gorm.DB {
		return tx.Where("(account_id = ? AND remote_key_id = ?) OR channel_id = ?", b.AccountID, b.RemoteKeyID, b.ChannelID)
	}
	if err := matching().Where("(channel_id, channel_created_time) NOT IN (?)", current).Delete(&UpstreamChannelBinding{}).Error; err != nil {
		return err
	}
	var existing []UpstreamChannelBinding
	if err := lockForUpdate(matching()).Find(&existing).Error; err != nil {
		return err
	}
	if len(existing) > 0 {
		return ErrRemoteKeyAlreadyBound
	}
	return tx.Create(b).Error
}

func BindUpstreamChannel(b *UpstreamChannelBinding, verify func(*Channel) error) error {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lock := sqliteChannelKeyLock(b.ChannelID)
		lock.Lock()
		defer lock.Unlock()
	}
	return withUpstreamAccountLocked(b.AccountID, func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).First(&channel, b.ChannelID).Error; err != nil {
			return err
		}
		if b.ChannelCreatedTime != channel.CreatedTime {
			return ErrUpstreamRatioConflict
		}
		if err := verify(&channel); err != nil {
			return err
		}
		return insertUpstreamBinding(tx, b)
	})
}

func CreateBoundUpstreamChannel(b *UpstreamChannelBinding, ch *Channel, fingerprint func(*Channel) string) error {
	err := withUpstreamAccountLocked(b.AccountID, func(tx *gorm.DB) error {
		if err := tx.Create(ch).Error; err != nil {
			return err
		}
		if err := ch.AddAbilities(tx); err != nil {
			return err
		}
		b.ChannelID, b.ChannelCreatedTime, b.ChannelFP = ch.Id, ch.CreatedTime, fingerprint(ch)
		return insertUpstreamBinding(tx, b)
	})
	if err != nil {
		return err
	}
	CacheUpdateChannel(ch)
	BumpSyncTopic(SyncTopicChannels)
	return nil
}

func UpdateChannelRatioFromUpstreamSyncLocked(channelID int, expectedCreatedTime int64, verify func(*Channel) error, targetRatio string) (bool, error) {
	normalized, err := NormalizeChannelRatio(targetRatio)
	if err != nil {
		return false, err
	}
	// A concurrent cache reload must not publish an older snapshot after this write.
	if common.MemoryCacheEnabled {
		channelStatusLock.Lock()
		defer channelStatusLock.Unlock()
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lock := sqliteChannelKeyLock(channelID)
		lock.Lock()
		defer lock.Unlock()
	}
	changed := false
	err = channelBindingTransaction(upstreamDB(), func(tx *gorm.DB) error {
		var channel Channel
		if err := lockForUpdate(tx).First(&channel, channelID).Error; err != nil {
			return err
		}
		if err := verify(&channel); err != nil {
			return err
		}
		if channel.CreatedTime != expectedCreatedTime {
			return ErrUpstreamRatioConflict
		}
		if normalized == channel.ChannelRatio {
			return nil
		}
		changed = true
		return tx.Model(&Channel{}).Where("id = ?", channelID).UpdateColumn("channel_ratio", normalized).Error
	})
	if err != nil || !changed {
		return false, err
	}
	CacheUpdateChannelRatio(channelID, normalized)
	BumpSyncTopic(SyncTopicChannels)
	return true, nil
}

func CacheUpdateChannelRatio(channelID int, normalized string) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if cached := channelsIDM[channelID]; cached != nil {
		updated := *cached
		updated.ChannelRatio = normalized
		channelsIDM[channelID] = &updated
	}
}

// Select IDs only. The minute job reloads account and binding state under the account lock.
func Sub2APIRatioAccountIDs() ([]int, error) {
	accounts := DB.Model(&UpstreamAccount{}).Select("id").Where("platform = ? AND status = ? AND last_sync_at > 0 AND last_error = ?", UpstreamRatioSourceSub2API, "active", "")
	var ids []int
	err := DB.Model(&UpstreamChannelBinding{}).Distinct("account_id").Where("stale = ? AND ratio_sync_enabled = ? AND remote_status = ? AND account_id IN (?)", false, true, "active", accounts).Order("account_id").Pluck("account_id", &ids).Error
	return ids, err
}

var ErrUpstreamBindingStale = errors.New("channel_changed")

type upstreamBindingMismatch struct{ binding UpstreamChannelBinding }

func (e *upstreamBindingMismatch) Error() string { return ErrUpstreamBindingStale.Error() }

func (b UpstreamChannelBinding) Verify(ch *Channel) error {
	if ch == nil || ch.CreatedTime != b.ChannelCreatedTime || UpstreamRatioFingerprint(ch) != b.ChannelFP {
		return &upstreamBindingMismatch{b}
	}
	return nil
}

// A rejected operation commits only the stale flag. Callers receive the domain
// error after commit. Database/commit failures always take precedence.
func channelBindingTransaction(db *gorm.DB, fn func(*gorm.DB) error) error {
	stale := false
	err := db.Transaction(func(tx *gorm.DB) error {
		err := fn(tx)
		mismatch, ok := err.(*upstreamBindingMismatch)
		if !ok {
			return err
		}
		stale = true
		b := mismatch.binding
		marked := tx.Model(&UpstreamChannelBinding{}).Where("id = ? AND stale = ? AND channel_fp = ? AND channel_created_time = ?", b.ID, false, b.ChannelFP, b.ChannelCreatedTime).Update("stale", true)
		if marked.Error != nil || marked.RowsAffected == 0 {
			return marked.Error
		}
		return tx.Model(&ChannelUpstreamRatio{}).Where("channel_id = ? AND mode = ? AND mapping = ? AND channel_created_time = ? AND credential_fp = ?", b.ChannelID, UpstreamRatioModeAuto, "account", b.ChannelCreatedTime, b.ChannelFP).Updates(map[string]any{"next_probe_at": 0, "version": gorm.Expr("version + 1")}).Error
	})
	if err == nil && stale {
		return ErrUpstreamBindingStale
	}
	return err
}

// ValidateUpstreamBinding commits an identity mismatch under the channel lock,
// including when the channel was deleted between enumeration and verification.
func ValidateUpstreamBinding(b UpstreamChannelBinding) error {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lock := sqliteChannelKeyLock(b.ChannelID)
		lock.Lock()
		defer lock.Unlock()
	}
	return validateUpstreamBindingDB(b)
}

func validateUpstreamBindingDB(b UpstreamChannelBinding) error {
	return channelBindingTransaction(upstreamDB(), func(tx *gorm.DB) error {
		var ch Channel
		err := lockForUpdate(tx).First(&ch, b.ChannelID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return b.Verify(nil)
		}
		if err != nil {
			return err
		}
		return b.Verify(&ch)
	})
}

// CheckUpstreamBindingIdentities checks even accounts whose next remote sync is
// delayed. A changed identity is re-read under the channel lock before writing.
func CheckUpstreamBindingIdentities(ctx context.Context, accountID int) error {
	db := upstreamDB().WithContext(ctx)
	query := db.Where("stale = ?", false)
	if accountID > 0 {
		query = query.Where("account_id = ?", accountID)
	}
	var batch []UpstreamChannelBinding
	return query.FindInBatches(&batch, 128, func(_ *gorm.DB, _ int) error {
		ids := make([]int, 0, len(batch))
		for _, b := range batch {
			ids = append(ids, b.ChannelID)
		}
		var channels []Channel
		if err := db.Select("id", "created_time", "type", "base_url", "key", "setting").Where("id IN ?", ids).Find(&channels).Error; err != nil {
			return err
		}
		byID := make(map[int]*Channel, len(channels))
		for i := range channels {
			ch := &channels[i]
			byID[ch.Id] = ch
		}
		for _, b := range batch {
			if b.Verify(byID[b.ChannelID]) == nil {
				continue
			}
			if err := ValidateUpstreamBinding(b); err != nil && !errors.Is(err, ErrUpstreamBindingStale) {
				return err
			}
		}
		return nil
	}).Error
}
