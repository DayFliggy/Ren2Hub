package billing

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billingunits"
	"github.com/shopspring/decimal"
)

func lookupPriceUnit(unit string) (billingunits.Definition, bool) {
	name, ok := strings.CutPrefix(unit, "per_")
	if !ok {
		return billingunits.Definition{}, false
	}
	return billingunits.Lookup(name)
}

func unitDivisor(unit string) (decimal.Decimal, error) {
	entry, ok := lookupPriceUnit(unit)
	if !ok {
		return decimal.Zero, &PricingError{Code: "unsupported_price_unit", Dimension: unit}
	}
	return decimal.NewFromString(entry.Scale)
}

func quantityUnitAllowed(quantity, unit string) bool {
	entry, ok := lookupPriceUnit(unit)
	if !ok {
		return false
	}
	if tokenDimension(quantity) {
		return unit == "per_million_tokens"
	}
	if strings.HasPrefix(quantity, "tool:") {
		return entry.Dimension == "call"
	}
	want := map[string]string{"calls": "call", "images": "image", "seconds": "time", "characters": "character", "credits": "credit"}[quantity]
	return want != "" && want == entry.Dimension
}

// ConvertUsageUnit only converts units within the same physical dimension.
func ConvertUsageUnit(value, from, to string) (string, error) {
	a, aok := billingunits.Lookup(from)
	b, bok := billingunits.Lookup(to)
	if !aok || !bok || a.Dimension != b.Dimension {
		return "", fmt.Errorf("incompatible usage units")
	}
	number, err := ParseAmountUSD(value)
	if err != nil {
		return "", err
	}
	x, _ := decimal.NewFromString(a.Scale)
	y, _ := decimal.NewFromString(b.Scale)
	return number.Mul(x).DivRound(y, 36).String(), nil
}

type PriceMetering struct {
	RequestKind         string `json:"request_kind"`
	Quantity            string `json:"quantity"`
	Source              string `json:"source"`
	Method              string `json:"method"`
	ReservationQuantity string `json:"reservation_quantity,omitempty"`
}

func validatePriceMetering(version PriceVersion) error {
	contract := version.Metering
	if contract == nil {
		for _, price := range version.Prices {
			if price.Quantity == "characters" && version.UsageSchema == nil {
				return &PricingError{Code: "missing_usage_contract", Dimension: "characters"}
			}
		}
		return nil
	}
	valid := contract.RequestKind == "speech" && contract.Quantity == "characters" &&
		(contract.Source == "request_fixed" && contract.Method == "unicode_codepoints" || contract.Source == "completion_fact" && contract.Method == "provider_usage") ||
		contract.RequestKind == "transcription" && contract.Quantity == "seconds" && contract.Source == "completion_fact" && contract.Method == "provider_duration"
	if !valid || version.UsageSchema != nil || version.Mode != ModeUnitPrice {
		return &PricingError{Code: "missing_usage_contract", Dimension: contract.Quantity}
	}
	if contract.Source == "completion_fact" {
		reserve, err := ParseAmountUSD(contract.ReservationQuantity)
		if err != nil || !reserve.IsPositive() {
			return fmt.Errorf("completion metering requires a reviewed reservation quantity")
		}
	} else if contract.ReservationQuantity != "" {
		return fmt.Errorf("request-fixed metering cannot override its exact reservation quantity")
	}
	found := false
	for _, price := range version.Prices {
		found = found || price.Quantity == contract.Quantity
		if price.Quantity != contract.Quantity && price.Quantity != "calls" {
			return fmt.Errorf("audio metering cannot mix token and physical units")
		}
	}
	if !found {
		return &PricingError{Code: "missing_price_dimension", Dimension: contract.Quantity}
	}
	return nil
}
