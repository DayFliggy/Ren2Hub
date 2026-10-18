package model

import (
	"context"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/shopspring/decimal"
)

type CatalogPrice struct {
	PriceStatus   string            `json:"price_status"`
	PricingValues map[string]string `json:"pricing_values"`
	PriceReason   string            `json:"price_reason,omitempty"`
}

func CatalogPrices(ctx context.Context, names []string) (map[string]CatalogPrice, error) {
	values, existing, _, err := readModelPricingMaps(DB.WithContext(ctx))
	if err != nil {
		return nil, err
	}
	result := make(map[string]CatalogPrice, len(names))
	for _, name := range names {
		effective := effectiveModelPricing(values, name)
		price := catalogPrice(effective)
		if price.PriceStatus != "unavailable" && price.PriceStatus != "zero" {
			price.PriceStatus = "default"
			alias := ratio_setting.FormatMatchingModelName(name)
			for _, key := range []string{"ModelPrice", "ModelRatio", "CompletionRatio", "billing_setting.billing_mode", "billing_setting.billing_expr"} {
				if !existing[key] {
					continue
				}
				_, exact := values[key][name]
				_, matched := values[key][alias]
				if exact || matched {
					price.PriceStatus = "configured"
					break
				}
			}
		}
		result[name] = price
	}
	return result, nil
}
func catalogPrice(values PricingValues) CatalogPrice {
	result := CatalogPrice{PriceStatus: "unavailable", PricingValues: map[string]string{}, PriceReason: "price_not_configured"}
	if values["billing_setting.billing_mode"] == "tiered_expr" {
		expression, _ := values["billing_setting.billing_expr"].(string)
		prices, ok := billingexpr.StaticReferencePrices(expression)
		if !ok {
			result.PriceReason = "expression_requires_usage_context"
			return result
		}
		result.PricingValues = prices
		return classifyCatalogPrice(result)
	}
	number := func(key string) (decimal.Decimal, bool) {
		raw, ok := values[key].(float64)
		if !ok || raw < 0 || math.IsNaN(raw) || math.IsInf(raw, 0) {
			return decimal.Zero, false
		}
		return decimal.NewFromFloat(raw), true
	}
	if fixed, ok := number("ModelPrice"); ok {
		result.PricingValues["request"] = fixed.String()
	} else if ratio, ok := number("ModelRatio"); ok {
		if common.QuotaPerUnit <= 0 || math.IsNaN(common.QuotaPerUnit) || math.IsInf(common.QuotaPerUnit, 0) {
			result.PriceReason = "invalid_quota_unit"
			return result
		}
		input := ratio.Mul(decimal.NewFromInt(1_000_000)).Div(decimal.NewFromFloat(common.QuotaPerUnit))
		result.PricingValues["input"] = input.String()
		if completion, ok := number("CompletionRatio"); ok {
			result.PricingValues["output"] = input.Mul(completion).String()
		}
	} else {
		return result
	}
	return classifyCatalogPrice(result)
}
func classifyCatalogPrice(result CatalogPrice) CatalogPrice {
	result.PriceStatus = "default"
	result.PriceReason = ""
	zero := true
	for _, value := range result.PricingValues {
		v, _ := decimal.NewFromString(value)
		if !v.IsZero() {
			zero = false
		}
	}
	if zero {
		result.PriceStatus = "zero"
	}
	return result
}
func ScaleCatalogPrice(base CatalogPrice, ratio string) (CatalogPrice, error) {
	factor, err := decimal.NewFromString(ratio)
	if err != nil || !factor.IsPositive() {
		return CatalogPrice{}, fmt.Errorf("invalid channel ratio")
	}
	result := base
	result.PricingValues = make(map[string]string, len(base.PricingValues))
	for key, value := range base.PricingValues {
		price, err := decimal.NewFromString(value)
		if err != nil {
			return CatalogPrice{}, err
		}
		result.PricingValues[key] = price.Mul(factor).String()
	}
	return result, nil
}
