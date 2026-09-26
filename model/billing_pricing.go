package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBillingPriceConflict = errors.New("billing price version conflict")
var ErrAutomaticBillingPriceUnavailable = errors.New("automatic billing price is unavailable")

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
	Model       string `gorm:"type:varchar(255);primaryKey" json:"model"`
	VersionID   int64  `gorm:"type:bigint;not null" json:"version_id"`
	Origin      string `gorm:"type:varchar(32)" json:"origin"`
	SourceModel string `gorm:"type:varchar(255)" json:"source_model"`
	SourceHash  string `gorm:"type:varchar(64)" json:"source_hash"`
}

func (*BillingPriceVersion) BeforeUpdate(*gorm.DB) error {
	return errors.New("published billing price versions are immutable")
}
func (*BillingPriceVersion) BeforeDelete(*gorm.DB) error {
	return errors.New("published billing price versions are immutable")
}

// Model identifiers are case-sensitive on the wire and in frozen price hashes.
// Existing MySQL databases commonly inherit a case-insensitive default collation.
func migrateBillingPriceModelIdentity(db *gorm.DB) error {
	if !common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return nil
	}
	for _, table := range []string{"billing_active_prices", "billing_price_versions"} {
		var collation string
		if err := db.Raw(`SELECT COLLATION_NAME FROM information_schema.columns
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`, table, "model").Scan(&collation).Error; err != nil {
			return err
		}
		if collation == "utf8mb4_bin" {
			continue
		}
		if collation == "" {
			return fmt.Errorf("billing model identity column missing in %s", table)
		}
		if err := db.Exec("ALTER TABLE `" + table + "` MODIFY COLUMN `model` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL").Error; err != nil {
			return err
		}
	}
	return nil
}

func PublishBillingPrice(ctx context.Context, version billing.PriceVersion, expectedVersion int64, actor int) (*billing.PriceVersion, error) {
	result, _, err := publishBillingPrice(ctx, version, billingPricePublication{
		origin: BillingPriceOriginManual, expectedVersion: &expectedVersion, actor: actor,
	})
	return result, err
}

type billingPricePublication struct {
	origin          string
	sourceModel     string
	sourceHash      string
	expectedVersion *int64
	expectedCatalog map[string]string
	actor           int
	automatic       bool
}

func publishBillingPrice(ctx context.Context, version billing.PriceVersion, publication billingPricePublication) (*billing.PriceVersion, bool, error) {
	if DB == nil {
		return nil, false, fmt.Errorf("billing price database unavailable")
	}
	var saved *billing.PriceVersion
	var published bool
	err := DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if publication.automatic {
			catalog := publication.expectedCatalog
			if catalog == nil {
				catalog = map[string]string{publication.origin: publication.sourceHash}
			} else if catalog[publication.origin] != publication.sourceHash {
				return ErrBillingPricingSourceConflict
			}
			if err := lockAutomaticBillingPricingSources(tx, catalog); err != nil {
				return err
			}
		}
		var err error
		saved, published, err = publishBillingPriceWithDB(tx, version, publication)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return saved, published, nil
}

// The caller owns the transaction and, for automatic prices, source locks.
// Batch publication shares this path with individual manual and automatic edits.
func publishBillingPriceWithDB(tx *gorm.DB, version billing.PriceVersion, publication billingPricePublication) (*billing.PriceVersion, bool, error) {
	version.Model = strings.TrimSpace(version.Model)
	if err := reasoning.ValidateBillingModelName(version.Model); err != nil {
		return nil, false, err
	}
	version.Prices = append(make([]billing.UnitPrice, 0, len(version.Prices)), version.Prices...)
	version.ID = 0
	version.CreatedAt = 0
	version.Hash = ""
	version, err := billing.PreparePriceVersion(version)
	if err != nil {
		return nil, false, err
	}
	for i := range version.Prices {
		price, err := decimal.NewFromString(version.Prices[i].Price)
		if err != nil {
			return nil, false, err
		}
		version.Prices[i].Price = price.String()
	}
	definition, err := common.Marshal(version)
	if err != nil {
		return nil, false, err
	}
	hash, err := billing.Hash(version)
	if err != nil {
		return nil, false, err
	}
	active := BillingActivePrice{Model: version.Model}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&active).Error; err != nil {
		return nil, false, err
	}
	if err := lockForUpdate(tx).Where("model = ?", version.Model).First(&active).Error; err != nil {
		return nil, false, err
	}
	if publication.expectedVersion != nil && active.VersionID != *publication.expectedVersion {
		return nil, false, ErrBillingPriceConflict
	}
	if publication.automatic && active.VersionID != 0 {
		var current BillingPriceVersion
		if err := tx.First(&current, active.VersionID).Error; err != nil {
			return nil, false, err
		}
		origin := resolveBillingPriceOrigin(active.Origin)
		// Only explicit restore may replace a manually owned price. This
		// decision uses the locked pointer, never a caller's stale listing.
		if publication.expectedVersion == nil && origin == BillingPriceOriginManual {
			return nil, false, nil
		}
		if _, err := decodeBillingPrice(current); err != nil {
			return nil, false, err
		}
		if current.Model != version.Model {
			return nil, false, fmt.Errorf("billing price model integrity mismatch")
		}
		if current.Hash == hash {
			if err := updateBillingPriceOrigin(tx, active, active.VersionID, publication); err != nil {
				return nil, false, err
			}
			version.ID, version.CreatedAt, version.Hash = current.ID, current.CreatedAt, current.Hash
			return &version, false, nil
		}
	}
	saved := BillingPriceVersion{Model: version.Model, Definition: string(definition), Hash: hash, CreatedAt: time.Now().Unix(), CreatedBy: publication.actor}
	if err := tx.Create(&saved).Error; err != nil {
		return nil, false, err
	}
	if err := updateBillingPriceOrigin(tx, active, saved.ID, publication); err != nil {
		return nil, false, err
	}
	version.ID = saved.ID
	version.CreatedAt = saved.CreatedAt
	version.Hash = saved.Hash
	return &version, true, nil
}

func updateBillingPriceOrigin(tx *gorm.DB, active BillingActivePrice, versionID int64, publication billingPricePublication) error {
	if active.VersionID == versionID && active.Origin == publication.origin && active.SourceModel == publication.sourceModel && active.SourceHash == publication.sourceHash {
		return nil
	}
	updated := tx.Model(&BillingActivePrice{}).Where("model = ? AND version_id = ?", active.Model, active.VersionID).
		Updates(map[string]interface{}{"version_id": versionID, "origin": publication.origin, "source_model": publication.sourceModel, "source_hash": publication.sourceHash})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return ErrBillingPriceConflict
	}
	return nil
}

func decodeBillingPrice(row BillingPriceVersion) (*billing.PriceVersion, error) {
	var version billing.PriceVersion
	if err := common.UnmarshalJsonStr(row.Definition, &version); err != nil {
		return nil, err
	}
	if err := billing.ValidateFrozenPriceVersion(version); err != nil {
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
	var row struct {
		BillingPriceVersion
		ActiveVersionID int64
		Origin          string
	}
	err := DB.WithContext(ctx).Table("billing_active_prices").
		Select("billing_price_versions.*, billing_active_prices.version_id AS active_version_id, billing_active_prices.origin").
		Joins("LEFT JOIN billing_price_versions ON billing_active_prices.version_id = billing_price_versions.id").
		Where("billing_active_prices.model = ?", modelName).Take(&row).Error
	if err != nil {
		return nil, err
	}
	if row.ActiveVersionID == 0 && row.Origin == BillingPriceOriginUnavailable {
		return nil, ErrAutomaticBillingPriceUnavailable
	}
	if row.ID == 0 || row.ActiveVersionID != row.ID || row.Model != modelName {
		return nil, fmt.Errorf("billing price model integrity mismatch")
	}
	return decodeBillingPrice(row.BillingPriceVersion)
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
