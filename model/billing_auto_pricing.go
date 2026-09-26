package model

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingPriceOriginManual      = "manual"
	BillingPriceOriginSub2API     = "sub2api"
	BillingPriceOriginAxonHub     = "axonhub"
	BillingPriceOriginModelsDev   = "models_dev"
	BillingPriceOriginOpenRouter  = "openrouter"
	BillingPriceOriginLegacySeed  = "legacy_seed"
	BillingPriceOriginUnavailable = "unavailable"
)

var ErrBillingPricingSourceConflict = errors.New("billing pricing source changed during synchronization")

// All publications lock this order so two source selections cannot deadlock.
var automaticBillingPricingSourceOrder = [...]string{
	BillingPriceOriginSub2API, BillingPriceOriginAxonHub, BillingPriceOriginModelsDev, BillingPriceOriginOpenRouter,
}

type BillingPriceOrigin struct {
	Model       string `json:"model"`
	VersionID   int64  `json:"version_id"`
	Origin      string `json:"origin"`
	SourceModel string `json:"source_model"`
	SourceHash  string `json:"source_hash"`
}

type BillingPricingSource struct {
	Source       string   `gorm:"type:varchar(32);primaryKey" json:"source"`
	Document     LongText `json:"-"`
	Hash         string   `gorm:"type:varchar(64)" json:"hash"`
	ETag         string   `gorm:"column:etag;type:text" json:"etag"`
	LastModified string   `gorm:"type:text" json:"last_modified"`
	CheckedAt    int64    `gorm:"type:bigint;not null" json:"checked_at"`
	UpdatedAt    int64    `gorm:"type:bigint;not null" json:"updated_at"`
	LastError    string   `gorm:"type:text" json:"last_error"`
	ModelCount   int      `gorm:"not null" json:"model_count"`
}

type BillingPricingConfig struct {
	ID                   int  `gorm:"primaryKey;autoIncrement:false" json:"-"`
	Enabled              bool `gorm:"not null" json:"enabled"`
	CheckIntervalMinutes int  `gorm:"not null" json:"check_interval_minutes"`
}

type AutomaticBillingPrice struct {
	Price       billing.PriceVersion
	Source      string
	SourceModel string
	SourceHash  string
}

type AutomaticBillingPriceDeactivation struct {
	Model             string
	ExpectedVersionID int64
}

func isAutomaticBillingPriceSource(source string) bool {
	switch source {
	case BillingPriceOriginSub2API, BillingPriceOriginAxonHub, BillingPriceOriginModelsDev, BillingPriceOriginOpenRouter:
		return true
	default:
		return false
	}
}

func validateAutomaticBillingPriceSource(source, sourceModel, sourceHash string) error {
	if !isAutomaticBillingPriceSource(source) {
		return fmt.Errorf("invalid automatic billing price source")
	}
	if sourceModel == "" || sourceModel != strings.TrimSpace(sourceModel) || len(sourceModel) > 255 {
		return fmt.Errorf("invalid billing price source model")
	}
	if decoded, err := hex.DecodeString(sourceHash); err != nil || len(decoded) != 32 {
		return fmt.Errorf("invalid billing price source hash")
	}
	return nil
}

// PublishAutomaticBillingPrice appends only changed, automatically owned prices.
// Source attribution lives on the active pointer and is not part of frozen quotes.
func PublishAutomaticBillingPrice(ctx context.Context, version billing.PriceVersion, source, sourceModel, sourceHash string, expectedCatalog ...map[string]string) (bool, error) {
	if err := validateAutomaticBillingPriceSource(source, sourceModel, sourceHash); err != nil {
		return false, err
	}
	catalog, err := automaticBillingPricingCatalogFence(expectedCatalog)
	if err != nil {
		return false, err
	}
	_, published, err := publishBillingPrice(ctx, version, billingPricePublication{
		origin: source, sourceModel: sourceModel, sourceHash: sourceHash, automatic: true, expectedCatalog: catalog,
	})
	return published, err
}

// PublishAutomaticBillingPrices commits a bounded batch using one transaction.
// Every entry shares the same catalog fence; any error rolls back the whole batch.
func PublishAutomaticBillingPrices(ctx context.Context, entries []AutomaticBillingPrice, expectedCatalog map[string]string) (int, error) {
	if DB == nil {
		return 0, fmt.Errorf("billing price database unavailable")
	}
	if len(entries) > 100 {
		return 0, fmt.Errorf("automatic billing price batch exceeds 100 entries")
	}
	catalog, err := automaticBillingPricingCatalogFence([]map[string]string{expectedCatalog})
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if err := validateAutomaticBillingPriceSource(entry.Source, entry.SourceModel, entry.SourceHash); err != nil {
			return 0, err
		}
		if catalog[entry.Source] != entry.SourceHash {
			return 0, ErrBillingPricingSourceConflict
		}
	}
	if len(entries) == 0 {
		return 0, nil
	}
	published := 0
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAutomaticBillingPricingSources(tx, catalog); err != nil {
			return err
		}
		for _, entry := range entries {
			_, changed, err := publishBillingPriceWithDB(tx, entry.Price, billingPricePublication{
				origin: entry.Source, sourceModel: entry.SourceModel, sourceHash: entry.SourceHash, automatic: true,
			})
			if err != nil {
				return fmt.Errorf("publish automatic billing price %q: %w", entry.Price.Model, err)
			}
			if changed {
				published++
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return published, nil
}

// DeactivateAutomaticBillingPrices persists a zero-version unavailable marker.
// Quote lookup must stop at that identity instead of retrying a broader model.
// Immutable versions remain available to requests that already froze a quote.
func DeactivateAutomaticBillingPrices(ctx context.Context, entries []AutomaticBillingPriceDeactivation, expectedCatalog map[string]string) (int, error) {
	if DB == nil {
		return 0, fmt.Errorf("billing price database unavailable")
	}
	if len(entries) > 100 {
		return 0, fmt.Errorf("automatic billing price batch exceeds 100 entries")
	}
	catalog, err := automaticBillingPricingCatalogFence([]map[string]string{expectedCatalog})
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if err := reasoning.ValidateBillingModelName(entry.Model); err != nil {
			return 0, err
		}
		if entry.Model != strings.TrimSpace(entry.Model) || len(entry.Model) > 255 || entry.ExpectedVersionID < 0 {
			return 0, fmt.Errorf("invalid automatic billing price deactivation")
		}
	}
	if len(entries) == 0 {
		return 0, nil
	}
	deactivated := 0
	err = DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAutomaticBillingPricingSources(tx, catalog); err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.ExpectedVersionID == 0 {
				marker := BillingActivePrice{Model: entry.Model, Origin: BillingPriceOriginUnavailable}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker).Error; err != nil {
					return err
				}
			}
			var active BillingActivePrice
			err := lockForUpdate(tx).Where("model = ?", entry.Model).Take(&active).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if active.VersionID != entry.ExpectedVersionID {
				continue
			}
			if active.VersionID == 0 {
				continue
			}
			var current BillingPriceVersion
			if err := tx.First(&current, active.VersionID).Error; err != nil {
				return err
			}
			origin, err := resolveBillingPriceOrigin(active, current)
			if err != nil {
				return err
			}
			if !isAutomaticBillingPriceSource(origin) && origin != BillingPriceOriginLegacySeed {
				continue
			}
			if current.Model != entry.Model || active.Model != entry.Model {
				return fmt.Errorf("billing price model integrity mismatch")
			}
			updated := tx.Model(&BillingActivePrice{}).Where("model = ? AND version_id = ?", entry.Model, entry.ExpectedVersionID).
				Updates(map[string]interface{}{"version_id": 0, "origin": BillingPriceOriginUnavailable, "source_model": "", "source_hash": ""})
			if updated.Error != nil {
				return updated.Error
			}
			deactivated += int(updated.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deactivated, nil
}

func lockAutomaticBillingPricingSources(tx *gorm.DB, expected map[string]string) error {
	// Source-before-active is the shared lock order. Read only hashes because
	// cached source documents can be several megabytes each.
	for _, name := range automaticBillingPricingSourceOrder {
		hash, included := expected[name]
		if !included {
			continue
		}
		var source BillingPricingSource
		err := lockForUpdate(tx).Select("source", "hash").Where("source = ?", name).Take(&source).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrBillingPricingSourceConflict
		}
		if err != nil {
			return err
		}
		if source.Hash != hash {
			return ErrBillingPricingSourceConflict
		}
	}
	return nil
}

// RestoreAutomaticBillingPrice is an explicit administrator decision. Its CAS
// fence prevents a stale restore from replacing a more recent manual price.
func RestoreAutomaticBillingPrice(ctx context.Context, version billing.PriceVersion, source, sourceModel, sourceHash string, expectedVersion int64, expectedCatalog ...map[string]string) (*billing.PriceVersion, error) {
	if err := validateAutomaticBillingPriceSource(source, sourceModel, sourceHash); err != nil {
		return nil, err
	}
	catalog, err := automaticBillingPricingCatalogFence(expectedCatalog)
	if err != nil {
		return nil, err
	}
	result, _, err := publishBillingPrice(ctx, version, billingPricePublication{
		origin: source, sourceModel: sourceModel, sourceHash: sourceHash, automatic: true, expectedVersion: &expectedVersion, expectedCatalog: catalog,
	})
	return result, err
}

func automaticBillingPricingCatalogFence(expected []map[string]string) (map[string]string, error) {
	if len(expected) == 0 {
		return nil, nil
	}
	if len(expected) != 1 || len(expected[0]) != len(automaticBillingPricingSourceOrder) {
		return nil, ErrBillingPricingSourceConflict
	}
	copy := make(map[string]string, len(automaticBillingPricingSourceOrder))
	for _, source := range automaticBillingPricingSourceOrder {
		hash, exists := expected[0][source]
		if !exists {
			return nil, ErrBillingPricingSourceConflict
		}
		copy[source] = hash
	}
	return copy, nil
}

var historicalBillingSeedHashes = sync.OnceValues(func() (map[string]string, error) {
	versions, _, err := InitialBillingPriceCatalog()
	if err != nil {
		return nil, err
	}
	hashes := make(map[string]string, len(versions))
	for _, version := range versions {
		hash, err := billing.Hash(version)
		if err != nil {
			return nil, err
		}
		hashes[version.Model] = hash
	}
	return hashes, nil
})

func resolveBillingPriceOrigin(active BillingActivePrice, row BillingPriceVersion) (string, error) {
	if isAutomaticBillingPriceSource(active.Origin) || active.Origin == BillingPriceOriginLegacySeed {
		return active.Origin, nil
	}
	if active.Origin != "" || row.CreatedBy != 0 {
		return BillingPriceOriginManual, nil
	}
	hashes, err := historicalBillingSeedHashes()
	if err != nil {
		return "", err
	}
	if hash, exists := hashes[active.Model]; !exists || hash != row.Hash || row.Model != active.Model {
		return BillingPriceOriginManual, nil
	}
	// A matching hash column alone is insufficient: verify the stored immutable
	// definition too before taking ownership of an unlabelled historical price.
	if _, err := decodeBillingPrice(row); err != nil {
		return "", err
	}
	return BillingPriceOriginLegacySeed, nil
}

func ListBillingPriceOrigins(ctx context.Context) ([]BillingPriceOrigin, error) {
	if DB == nil {
		return nil, fmt.Errorf("billing price database unavailable")
	}
	var rows []struct {
		BillingActivePrice
		Definition   string
		Hash         string
		CreatedBy    int
		CreatedAt    int64
		VersionModel string
	}
	err := DB.WithContext(ctx).Table("billing_active_prices").
		Select("billing_active_prices.*, billing_price_versions.definition, billing_price_versions.hash, billing_price_versions.created_by, billing_price_versions.created_at, billing_price_versions.model AS version_model").
		Joins("JOIN billing_price_versions ON billing_price_versions.id = billing_active_prices.version_id").
		Order("billing_active_prices.model").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	origins := make([]BillingPriceOrigin, 0, len(rows))
	for _, row := range rows {
		origin, err := resolveBillingPriceOrigin(row.BillingActivePrice, BillingPriceVersion{
			ID: row.VersionID, Model: row.VersionModel, Definition: row.Definition, Hash: row.Hash, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt,
		})
		if err != nil {
			return nil, err
		}
		origins = append(origins, BillingPriceOrigin{
			Model: row.Model, VersionID: row.VersionID, Origin: origin, SourceModel: row.SourceModel, SourceHash: row.SourceHash,
		})
	}
	return origins, nil
}

// ListAutomaticBillingModelNames includes channel aliases and published prices.
// Removing a channel must not leave an already managed alias on an old price.
func ListAutomaticBillingModelNames(ctx context.Context) ([]string, error) {
	if DB == nil {
		return nil, fmt.Errorf("billing price database unavailable")
	}
	var configured, active []string
	if err := DB.WithContext(ctx).Model(&Ability{}).Distinct("model").Pluck("model", &configured).Error; err != nil {
		return nil, err
	}
	if err := DB.WithContext(ctx).Model(&BillingActivePrice{}).Pluck("model", &active).Error; err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(configured)+len(active))
	for _, name := range append(configured, active...) {
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func ListBillingPricingSources(ctx context.Context) ([]BillingPricingSource, error) {
	if DB == nil {
		return nil, fmt.Errorf("billing price database unavailable")
	}
	rows := make([]BillingPricingSource, 0)
	err := DB.WithContext(ctx).Order("source").Find(&rows).Error
	return rows, err
}

func SaveBillingPricingSource(ctx context.Context, record *BillingPricingSource) error {
	if DB == nil {
		return fmt.Errorf("billing price database unavailable")
	}
	if record == nil || !isAutomaticBillingPriceSource(record.Source) || record.ModelCount < 0 {
		return fmt.Errorf("invalid billing pricing source")
	}
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source"}},
		DoUpdates: clause.AssignmentColumns([]string{"document", "hash", "etag", "last_modified", "checked_at", "updated_at", "last_error", "model_count"}),
	}).Create(record).Error
}

// SaveBillingPricingSourceCAS prevents a slow fetch or failed check from
// replacing a newer snapshot that another process has already persisted.
func SaveBillingPricingSourceCAS(ctx context.Context, record *BillingPricingSource, expectedHash string) error {
	if DB == nil {
		return fmt.Errorf("billing price database unavailable")
	}
	if record == nil || !isAutomaticBillingPriceSource(record.Source) || record.ModelCount < 0 {
		return fmt.Errorf("invalid billing pricing source")
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current BillingPricingSource
		err := lockForUpdate(tx).Select("source", "hash").Where("source = ?", record.Source).Take(&current).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if expectedHash != "" {
				return ErrBillingPricingSourceConflict
			}
			created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(record)
			if created.Error != nil {
				return created.Error
			}
			if created.RowsAffected != 1 {
				return ErrBillingPricingSourceConflict
			}
			return nil
		}
		if err != nil {
			return err
		}
		if current.Hash != expectedHash {
			return ErrBillingPricingSourceConflict
		}
		return tx.Model(&BillingPricingSource{}).Where("source = ?", record.Source).Updates(map[string]interface{}{
			"document": record.Document, "hash": record.Hash, "etag": record.ETag, "last_modified": record.LastModified,
			"checked_at": record.CheckedAt, "updated_at": record.UpdatedAt, "last_error": record.LastError, "model_count": record.ModelCount,
		}).Error
	})
}

// EnsureBillingPricingSource installs a bundled snapshot only when no persisted
// source exists, so concurrent startup cannot replace a downloaded catalog.
func EnsureBillingPricingSource(ctx context.Context, record *BillingPricingSource) error {
	if DB == nil {
		return fmt.Errorf("billing price database unavailable")
	}
	if record == nil || !isAutomaticBillingPriceSource(record.Source) || record.ModelCount < 0 {
		return fmt.Errorf("invalid billing pricing source")
	}
	return DB.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(record).Error
}

func GetBillingPricingConfig(ctx context.Context) (*BillingPricingConfig, error) {
	if DB == nil {
		return nil, fmt.Errorf("billing price database unavailable")
	}
	var config BillingPricingConfig
	err := DB.WithContext(ctx).Where("id = ?", 1).Take(&config).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &BillingPricingConfig{ID: 1, Enabled: true, CheckIntervalMinutes: 30}, nil
	}
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func SaveBillingPricingConfig(ctx context.Context, config *BillingPricingConfig) error {
	if DB == nil {
		return fmt.Errorf("billing price database unavailable")
	}
	if config == nil || (config.ID != 0 && config.ID != 1) || config.CheckIntervalMinutes < 5 || config.CheckIntervalMinutes > 1440 {
		return fmt.Errorf("billing pricing check interval must be between 5 and 1440 minutes")
	}
	copy := *config
	copy.ID = 1
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled", "check_interval_minutes"}),
	}).Create(&copy).Error
}
