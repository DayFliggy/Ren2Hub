package model

import (
	"errors"
	"maps"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type UpstreamAccount struct {
	ID                   int    `json:"id" gorm:"primaryKey"`
	Name                 string `json:"name" gorm:"size:255;not null"`
	Platform             string `json:"platform" gorm:"size:16;not null;<-:create"`
	BaseURL              string `json:"base_url" gorm:"type:text;not null"`
	ProxyURL             string `json:"-" gorm:"type:text"`
	CredentialUID        string `json:"-" gorm:"size:36;uniqueIndex;not null;<-:create"`
	CredentialCiphertext string `json:"-" gorm:"type:text"`
	Status               string `json:"status" gorm:"size:32;not null;index:,composite:due,priority:1"`
	GroupsJSON           string `json:"-" gorm:"type:text"`
	SubscriptionsJSON    string `json:"-" gorm:"type:text"`
	SiteMetaJSON         string `json:"-" gorm:"type:text"`
	LastError            string `json:"last_error" gorm:"size:96"`
	NextSyncAt           int64  `json:"next_sync_at" gorm:"index:,composite:due,priority:2"`
	LastSyncAt           int64  `json:"last_sync_at"`
	Version              int64  `json:"version" gorm:"not null;default:1"`
	CreatedAt            int64  `json:"created_at"`
	UpdatedAt            int64  `json:"updated_at"`
}

var ErrUpstreamAccountConflict = errors.New("upstream_account_changed")

func upstreamDB() *gorm.DB {
	return DB.Session(&gorm.Session{Logger: logger.Discard})
}

func CreateUpstreamAccount(a *UpstreamAccount) error {
	if a.CredentialUID == "" {
		a.CredentialUID = uuid.NewString()
	}
	return upstreamDB().Create(a).Error
}

func GetUpstreamAccount(id int) (*UpstreamAccount, error) {
	var account UpstreamAccount
	if err := upstreamDB().First(&account, id).Error; err != nil {
		return nil, err
	}
	return &account, nil
}

func ListUpstreamAccounts() ([]UpstreamAccount, error) {
	var accounts []UpstreamAccount
	err := upstreamDB().Omit("credential_ciphertext").Order("id").Find(&accounts).Error
	return accounts, err
}

func GetDueUpstreamAccounts(now int64, limit int) ([]UpstreamAccount, error) {
	var accounts []UpstreamAccount
	err := upstreamDB().Select("id").Where("status = ? AND next_sync_at <= ?", "active", now).Order("next_sync_at, id").Limit(limit).Find(&accounts).Error
	return accounts, err
}

func UpdateUpstreamAccountCAS(id int, version int64, updates map[string]any) error {
	fields := maps.Clone(updates)
	fields["version"] = version + 1
	_, baseChanged := updates["base_url"]
	_, proxyChanged := updates["proxy_url"]
	reset := baseChanged || proxyChanged
	if reset {
		fields["credential_ciphertext"] = ""
		fields["status"] = "reauth_required"
		fields["groups_json"] = ""
		fields["site_meta_json"] = ""
		fields["subscriptions_json"] = ""
		fields["last_sync_at"] = 0
		fields["last_error"] = ""
	}
	return upstreamDB().Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&UpstreamAccount{}).Where("id = ? AND version = ?", id, version).Omit("id", "credential_uid", "platform", "created_at").Updates(fields)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrUpstreamAccountConflict
		}
		if reset {
			return tx.Model(&UpstreamChannelBinding{}).Where("account_id = ?", id).Update("stale", true).Error
		}
		return nil
	})
}

var upstreamSQLiteLocks [64]sync.Mutex

// Account deletion and binding both lock the parent before touching its children.
func withUpstreamAccountLocked(id int, fn func(*gorm.DB) error) error {
	if id <= 0 {
		return gorm.ErrRecordNotFound
	}
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		lock := &upstreamSQLiteLocks[id%len(upstreamSQLiteLocks)]
		lock.Lock()
		defer lock.Unlock()
	}
	return upstreamDB().Transaction(func(tx *gorm.DB) error {
		var account UpstreamAccount
		if err := lockForUpdate(tx).Select("id").First(&account, id).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func DeleteUpstreamAccount(id int) error {
	return withUpstreamAccountLocked(id, func(tx *gorm.DB) error {
		if err := tx.Where("account_id = ?", id).Delete(&UpstreamChannelBinding{}).Error; err != nil {
			return err
		}
		return tx.Delete(&UpstreamAccount{}, id).Error
	})
}
