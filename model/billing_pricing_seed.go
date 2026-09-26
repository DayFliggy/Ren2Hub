package model

import (
	_ "embed"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

//go:embed billing_pricing_seed.json
var billingPriceSeed []byte

const BillingPriceSeedSource = "d04c118c8803f49e0c9bab74dcf5b5efeab9464a"

// InitialBillingPriceCatalog reads the frozen historical installation catalog
// only for the startup ownership migration. Never regenerate it or use it for
// new installs or live price lookup.
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
