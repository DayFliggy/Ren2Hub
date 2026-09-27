package router

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Fixture catalogs publish the same immutable versions as the administration API.
func publishGatewayFixturePrices(raw string, tokenPrice bool) error {
	var prices map[string]json.Number
	if err := common.UnmarshalJsonStr(raw, &prices); err != nil {
		return err
	}
	for name, value := range prices {
		price, err := decimal.NewFromString(value.String())
		if err != nil {
			return err
		}
		version := billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: price.String()}}}
		if tokenPrice {
			version.Prices = []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: price.Mul(decimal.NewFromInt(2)).String()}, {Quantity: "c", Unit: "per_million_tokens", Price: price.Mul(decimal.NewFromInt(2)).String()}}
		}
		if price.IsZero() {
			version.Mode, version.Prices = billing.ModeFree, nil
		}
		var expected int64
		current, err := model.GetActiveBillingPrice(context.Background(), name)
		if err == nil {
			expected = current.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if _, err := model.PublishBillingPrice(context.Background(), version, expected, 1); err != nil {
			return err
		}
	}
	return nil
}

func publishGatewayImagePrice(name string) error {
	current, err := model.GetActiveBillingPrice(context.Background(), name)
	if err != nil {
		return err
	}
	_, err = model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: name, Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "images", Unit: "per_image", Price: "0.001"}}}, current.ID, 1)
	return err
}

func addGatewayFixtureDimensions(name string, prices ...billing.UnitPrice) error {
	current, err := model.GetActiveBillingPrice(context.Background(), name)
	if err != nil {
		return err
	}
	version := billing.PriceVersion{Model: name, Mode: billing.ModeUnitPrice,
		Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test prices for enabled capabilities"},
		Prices:     append(append([]billing.UnitPrice(nil), current.Prices...), prices...)}
	_, err = model.PublishBillingPrice(context.Background(), version, current.ID, 1)
	return err
}
