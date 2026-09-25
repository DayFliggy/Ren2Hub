package model

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"gorm.io/gorm"
)

//go:embed billing_pricing_seed.json
var billingPriceSeed []byte

const BillingPriceSeedSource = "d04c118c8803f49e0c9bab74dcf5b5efeab9464a"

// InitialBillingPriceCatalog reads only the reviewed, checked-in price catalog.
// Mutable runtime options and downloaded catalogs cannot alter installation.
func InitialBillingPriceCatalog() ([]billing.PriceVersion, []string, error) {
	var catalog struct {
		Source       string                 `json:"source"`
		Prices       []billing.PriceVersion `json:"prices"`
		Unconfigured []string               `json:"unconfigured"`
	}
	if err := common.Unmarshal(billingPriceSeed, &catalog); err != nil {
		return nil, nil, err
	}
	if catalog.Source != BillingPriceSeedSource {
		return nil, nil, fmt.Errorf("unexpected billing seed source")
	}
	seen := map[string]bool{}
	for i := range catalog.Prices {
		name := catalog.Prices[i].Model
		if seen[name] {
			return nil, nil, fmt.Errorf("duplicate billing seed model")
		}
		seen[name] = true
		if err := reasoning.ValidateBillingModelName(name); err != nil {
			return nil, nil, err
		}
		if catalog.Prices[i].Prices == nil {
			catalog.Prices[i].Prices = []billing.UnitPrice{}
		}
		prepared, err := billing.PreparePriceVersion(catalog.Prices[i])
		if err != nil {
			return nil, nil, err
		}
		catalog.Prices[i] = prepared
	}
	return catalog.Prices, catalog.Unconfigured, nil
}

func SeedBillingPriceVersions(ctx context.Context) error {
	versions, _, err := InitialBillingPriceCatalog()
	if err != nil {
		return err
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&BillingPriceVersion{}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		for _, version := range versions {
			if err := billing.ValidateFrozenPriceVersion(version); err != nil {
				return err
			}
			definition, err := common.Marshal(version)
			if err != nil {
				return err
			}
			hash, err := billing.Hash(version)
			if err != nil {
				return err
			}
			row := BillingPriceVersion{Model: version.Model, Definition: string(definition), Hash: hash, CreatedAt: time.Now().Unix()}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := tx.Create(&BillingActivePrice{Model: version.Model, VersionID: row.ID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
