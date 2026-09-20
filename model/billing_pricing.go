package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBillingPriceConflict = errors.New("billing price version conflict")

// BillingPriceVersion rows are append-only. The active pointer is independently
// locked, so changing the catalog cannot change a dispatched request's quote.
type BillingPriceVersion struct {
	ID         int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	Model      string `gorm:"type:varchar(255);not null;index" json:"model"`
	Definition string `gorm:"type:text;not null" json:"-"`
	Hash       string `gorm:"type:varchar(64);not null" json:"hash"`
	CreatedAt  int64  `gorm:"type:bigint;not null" json:"created_at"`
	CreatedBy  int    `gorm:"not null" json:"created_by"`
}

type BillingActivePrice struct {
	Model     string `gorm:"type:varchar(255);primaryKey" json:"model"`
	VersionID int64  `gorm:"type:bigint;not null" json:"version_id"`
}

func (*BillingPriceVersion) BeforeUpdate(*gorm.DB) error {
	return errors.New("published billing price versions are immutable")
}
func (*BillingPriceVersion) BeforeDelete(*gorm.DB) error {
	return errors.New("published billing price versions are immutable")
}

func PublishBillingPrice(ctx context.Context, version billing.PriceVersion, expectedVersion int64, actor int) (*billing.PriceVersion, error) {
	version.Model = strings.TrimSpace(version.Model)
	version.Prices = append(make([]billing.UnitPrice, 0, len(version.Prices)), version.Prices...)
	version.ID = 0
	version.CreatedAt = 0
	version.Hash = ""
	if err := billing.ValidatePriceVersion(version); err != nil {
		return nil, err
	}
	for i := range version.Prices {
		price, err := decimal.NewFromString(version.Prices[i].Price)
		if err != nil {
			return nil, err
		}
		version.Prices[i].Price = price.String()
	}
	definition, err := common.Marshal(version)
	if err != nil {
		return nil, err
	}
	hash, err := billing.Hash(version)
	if err != nil {
		return nil, err
	}
	var saved BillingPriceVersion
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		active := BillingActivePrice{Model: version.Model}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&active).Error; err != nil {
			return err
		}
		if err := lockForUpdate(tx).Where("model = ?", version.Model).First(&active).Error; err != nil {
			return err
		}
		if active.VersionID != expectedVersion {
			return ErrBillingPriceConflict
		}
		saved = BillingPriceVersion{Model: version.Model, Definition: string(definition), Hash: hash, CreatedAt: time.Now().Unix(), CreatedBy: actor}
		if err := tx.Create(&saved).Error; err != nil {
			return err
		}
		updated := tx.Model(&BillingActivePrice{}).Where("model = ? AND version_id = ?", version.Model, expectedVersion).Update("version_id", saved.ID)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return ErrBillingPriceConflict
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	version.ID = saved.ID
	version.CreatedAt = saved.CreatedAt
	version.Hash = saved.Hash
	return &version, nil
}

func decodeBillingPrice(row BillingPriceVersion) (*billing.PriceVersion, error) {
	var version billing.PriceVersion
	if err := common.UnmarshalJsonStr(row.Definition, &version); err != nil {
		return nil, err
	}
	if err := billing.ValidatePriceVersion(version); err != nil {
		return nil, err
	}
	hash, err := billing.Hash(version)
	if err != nil || hash != row.Hash {
		return nil, fmt.Errorf("billing price integrity mismatch")
	}
	version.ID = row.ID
	version.CreatedAt = row.CreatedAt
	version.Hash = row.Hash
	return &version, nil
}

func GetActiveBillingPrice(ctx context.Context, modelName string) (*billing.PriceVersion, error) {
	if DB == nil {
		return nil, fmt.Errorf("billing price database unavailable")
	}
	var row BillingPriceVersion
	err := DB.WithContext(ctx).Table("billing_price_versions").Select("billing_price_versions.*").
		Joins("JOIN billing_active_prices ON billing_active_prices.version_id = billing_price_versions.id").
		Where("billing_active_prices.model = ?", modelName).Take(&row).Error
	if err != nil {
		return nil, err
	}
	return decodeBillingPrice(row)
}

func ListActiveBillingPrices(ctx context.Context) ([]billing.PriceVersion, error) {
	var rows []BillingPriceVersion
	if err := DB.WithContext(ctx).Table("billing_price_versions").Select("billing_price_versions.*").
		Joins("JOIN billing_active_prices ON billing_active_prices.version_id = billing_price_versions.id").Order("billing_price_versions.model").Find(&rows).Error; err != nil {
		return nil, err
	}
	prices := make([]billing.PriceVersion, 0, len(rows))
	for _, row := range rows {
		version, err := decodeBillingPrice(row)
		if err != nil {
			return nil, err
		}
		prices = append(prices, *version)
	}
	return prices, nil
}
