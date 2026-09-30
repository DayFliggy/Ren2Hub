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
