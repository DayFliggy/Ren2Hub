package pricesource

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

type rates map[string]decimal.Decimal
type priceTier struct {
	threshold int64
	inclusive bool
	prices    rates
}
type schedule struct {
	base  rates
	tiers []priceTier
}

var dimensions = []string{"p", "c", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao"}

func price(raw common.RawMessage, scale int32) (decimal.Decimal, error) {
	value := strings.TrimSpace(string(raw))
	if strings.HasPrefix(value, "\"") {
		value = stringValue(raw)
	}
	if len(value) == 0 || len(value) > 64 {
		return decimal.Zero, fmt.Errorf("missing or invalid numeric price")
	}
	number, err := decimal.NewFromString(value)
	if err != nil || number.IsNegative() || number.Exponent() < -30 || number.Exponent() > 18 {
		return decimal.Zero, fmt.Errorf("price must be finite, non-negative and within precision limits")
	}
	number = number.Shift(scale)
	if len(number.String()) > 64 {
		return decimal.Zero, fmt.Errorf("price exceeds precision limits")
	}
	return number, nil
}

// Tool fees can accompany the same model request. A model-only draft may
// exclude them only when every declared amount is explicitly zero.
func zeroToolPrice(raw common.RawMessage, depth int) bool {
	if depth > 16 {
		return false
	}
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		values, err := readObject(raw)
		if err != nil || len(values) == 0 {
			return false
		}
		for _, value := range values {
			if !zeroToolPrice(value, depth+1) {
				return false
			}
		}
		return true
	}
	value, err := price(raw, 0)
	return err == nil && value.IsZero()
}

func addRate(target rates, dimension string, raw common.RawMessage, scale int32) error {
	value, err := price(raw, scale)
	if err != nil {
		return err
	}
	if previous, exists := target[dimension]; exists && !previous.Equal(value) {
		return fmt.Errorf("conflicting prices for %s", dimension)
	}
	target[dimension] = value
	return nil
}

func overlay(base, patch rates) rates {
	result := make(rates, len(base)+len(patch))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range patch {
		result[key] = value
	}
	return result
}

func threshold(raw common.RawMessage) (int64, error) {
	value, err := price(raw, 0)
	if err != nil || !value.IsInteger() || !value.IsPositive() || value.GreaterThan(decimal.NewFromInt(1000000000)) {
		return 0, fmt.Errorf("invalid context threshold")
	}
	return value.IntPart(), nil
}

func equalRates(a, b rates) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		other, exists := b[key]
		if !exists || !value.Equal(other) {
			return false
		}
	}
	return true
}

func scheduleExpression(s schedule, embedding bool) (string, error) {
	if len(s.tiers) > 16 {
		return "", fmt.Errorf("more than 16 price tiers")
	}
	sort.Slice(s.tiers, func(i, j int) bool { return s.tiers[i].threshold < s.tiers[j].threshold })
	union := make(map[string]bool)
	for key := range s.base {
		union[key] = true
	}
	for _, tier := range s.tiers {
		for key := range tier.prices {
			union[key] = true
		}
	}
	current := s.base
	branches := make([]string, 0, len(s.tiers)+1)
	for i := 0; i <= len(s.tiers); i++ {
		if i > 0 {
			tier := s.tiers[i-1]
			if tier.threshold <= 0 || (i > 1 && tier.threshold == s.tiers[i-2].threshold) {
				return "", fmt.Errorf("duplicate or invalid context threshold")
			}
			current = overlay(current, tier.prices)
		}
		body, err := ratesExpression(current, union, embedding)
		if err != nil {
			return "", err
		}
		label := "base"
		if i > 0 {
			label = "context_" + strconv.FormatInt(s.tiers[i-1].threshold, 10)
		}
		branches = append(branches, "tier(\""+label+"\", "+body+")")
	}
	result := branches[len(branches)-1]
	for i := len(s.tiers) - 1; i >= 0; i-- {
		op := " <= "
		if s.tiers[i].inclusive {
			op = " < "
		}
		result = "len" + op + strconv.FormatInt(s.tiers[i].threshold, 10) + " ? (" + branches[i] + ") : (" + result + ")"
	}
	return result, nil
}

func ratesExpression(values rates, union map[string]bool, embedding bool) (string, error) {
	if _, ok := values["p"]; !ok {
		return "", fmt.Errorf("missing explicit input token price")
	}
	if _, ok := values["c"]; !ok && !embedding {
		return "", fmt.Errorf("missing explicit output token price")
	}
	// NewAPI does not meter a distinct reasoning category. Equal output rates
	// are equivalent; different rates require an unavailable usage contract.
	if value, exists := values["reasoning"]; exists {
		output, ok := values["c"]
		if !ok || !value.Equal(output) {
			return "", fmt.Errorf("separate reasoning price cannot be represented")
		}
	}
	var terms []string
	for _, dimension := range dimensions {
		if !union[dimension] {
			continue
		}
		value, exists := values[dimension]
		if !exists {
			return "", fmt.Errorf("incomplete price tier for %s", dimension)
		}
		if embedding && (dimension == "c" || dimension == "ao" || dimension == "img_o") {
			if !value.IsZero() {
				return "", fmt.Errorf("embedding contains output price")
			}
			continue
		}
		terms = append(terms, dimension+" * "+value.String())
	}
	return strings.Join(terms, " + "), nil
}
