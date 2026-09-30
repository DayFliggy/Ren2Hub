package router

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
)

// Persist the estimate barrier and encrypt actual evidence exactly as a relay does.
func evaluateGatewayFixtureUsage(requestID, attemptID string, quote *billing.Quote, estimate, usage billing.Usage) (*billing.Usage, *billing.ChargeResult, error) {
	estimate.Estimated, estimate.Complete, estimate.Terminal = true, false, ""
	estimate, estimatedCharge, err := model.EvaluateBilling(quote, estimate, billingexpr.RequestInput{}, requestID, attemptID, "estimate")
	if err != nil {
		return nil, nil, err
	}
	if err := model.PersistBillingEstimate(attemptID, estimate, estimatedCharge); err != nil {
		return nil, nil, err
	}
	actual, charge, err := model.EvaluateBilling(quote, usage, billingexpr.RequestInput{}, requestID, attemptID, "settlement")
	return &actual, charge, err
}

// Fixtures use the same sole pricing configuration as administrator saves.
func setGatewayModelPrice(name string, pricing model.PricingValues) error {
	snapshot, err := model.GetModelPricingSnapshot([]string{name})
	if err != nil {
		return err
	}
	expected := snapshot.EmptyVersion
	for _, entry := range snapshot.Entries {
		if entry.ModelName == name {
			expected = entry.Version
		}
	}
	return model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: name, ExpectedVersion: expected, Pricing: pricing}})
}

func setGatewayExpression(name, expression string) (*billing.PriceVersion, error) {
	if err := setGatewayModelPrice(name, model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}); err != nil {
		return nil, err
	}
	version, err := model.ResolveModelBillingPrice(context.Background(), name, "")
	if err != nil {
		return nil, err
	}
	return model.StoreBillingPriceEvidence(context.Background(), *version)
}

func configureGatewayFixturePrices(raw string, tokenPrice bool) error {
	var prices map[string]json.Number
	if err := common.UnmarshalJsonStr(raw, &prices); err != nil {
		return err
	}
	for name, value := range prices {
		pricing := model.PricingValues{"ModelPrice": value}
		if tokenPrice {
			pricing = model.PricingValues{"ModelRatio": value, "CompletionRatio": json.Number("1")}
		}
		if err := setGatewayModelPrice(name, pricing); err != nil {
			return err
		}
	}
	return nil
}

func configureGatewayImagePrice(name string) error {
	_, err := setGatewayExpression(name, `tier("images", fixed(0.001)) * image_count`)
	return err
}

func addGatewayFixtureDimensions(name string, prices ...billing.UnitPrice) error {
	snapshot, err := model.GetModelPricingSnapshot([]string{name})
	if err != nil {
		return err
	}
	values := model.PricingValues{}
	for _, entry := range snapshot.Entries {
		if entry.ModelName == name {
			values = entry.Configured
		}
	}
	for _, price := range prices {
		if strings.HasPrefix(price.Quantity, "tool:") {
			amount, err := decimal.NewFromString(price.Price)
			if err != nil {
				return err
			}
			if price.Unit == "per_call" {
				amount = amount.Mul(decimal.NewFromInt(1000))
			}
			toolPrices := operation_setting.ModelToolPrices(name)
			toolPrices[strings.TrimPrefix(price.Quantity, "tool:")] = amount.InexactFloat64()
			encoded, err := common.Marshal(toolPrices)
			if err != nil {
				return err
			}
			if err := model.UpdateOption(operation_setting.ToolPriceOptionKey, string(encoded)); err != nil {
				return err
			}
			continue
		}
		if price.Quantity == "ai" || price.Quantity == "ao" {
			if price.Price != "0" {
				return fmt.Errorf("fixture requires explicit audio ratio")
			}
			if price.Quantity == "ai" {
				values["AudioRatio"] = json.Number("0")
			} else {
				values["AudioCompletionRatio"] = json.Number("0")
			}
			continue
		}
		return fmt.Errorf("unsupported fixture dimension %s", price.Quantity)
	}
	return setGatewayModelPrice(name, values)
}
