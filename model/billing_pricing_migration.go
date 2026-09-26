package model

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/pkg/billing"
	"gorm.io/gorm"
)

// MigrateBillingPriceOrigins records ownership for installations predating
// explicit origins. It never changes a price or depends on automatic sync being
// enabled; requests and disabled installations keep their published versions.
func MigrateBillingPriceOrigins(ctx context.Context) error {
	if DB == nil {
		return fmt.Errorf("billing price database unavailable")
	}
	var names []string
	if err := DB.WithContext(ctx).Model(&BillingActivePrice{}).
		Where("origin = ? OR origin IS NULL", "").Order("model").Pluck("model", &names).Error; err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	seeds, _, err := InitialBillingPriceCatalog()
	if err != nil {
		return err
	}
	hashes := make(map[string]string, len(seeds))
	for _, seed := range seeds {
		hash, err := billing.Hash(seed)
		if err != nil {
			return err
		}
		hashes[seed.Model] = hash
	}
	for _, name := range names {
		if err := BillingTransaction(ctx, func(tx *gorm.DB) error {
			var active BillingActivePrice
			err := lockForUpdate(tx).Where("model = ?", name).Take(&active).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			// A concurrent manual publication owns the locked pointer, even if
			// the initial migration scan saw an unlabelled historical version.
			if active.Origin != "" {
				return nil
			}
			var version BillingPriceVersion
			if err := tx.First(&version, active.VersionID).Error; err != nil {
				return err
			}
			if version.Model != active.Model {
				return fmt.Errorf("billing price model integrity mismatch")
			}
			origin := BillingPriceOriginManual
			if hash, exists := hashes[name]; exists && version.CreatedBy == 0 && hash == version.Hash {
				// A copied hash does not prove that the stored definition is the
				// original seed. Verify it before allowing automatic adoption.
				if _, err := decodeBillingPrice(version); err != nil {
					return err
				}
				origin = BillingPriceOriginLegacySeed
			}
			return tx.Model(&BillingActivePrice{}).Where("model = ? AND version_id = ?", name, active.VersionID).
				Update("origin", origin).Error
		}); err != nil {
			return fmt.Errorf("migrate billing price origin %q: %w", name, err)
		}
	}
	return nil
}
