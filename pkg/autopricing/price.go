package autopricing

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/shopspring/decimal"
)

type rates map[string]decimal.Decimal
type priceTier struct {
	threshold int64
	inclusive bool
	rates     rates
}
type schedule struct {
	base  rates
	tiers []priceTier
}
type pricePlan struct {
	standard   schedule
	services   map[string]schedule
	extras     []billing.UnitPrice
	usageScope string
}

var dimensionOrder = []string{"p", "c", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao", "images"}

func amount(raw common.RawMessage, scale int32) (decimal.Decimal, error) {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "\"") {
		text = rawString(raw)
	}
	if len(text) == 0 || len(text) > 64 {
		return decimal.Zero, unsafePrice("invalid numeric price")
	}
	value, err := decimal.NewFromString(text)
	if err != nil || value.IsNegative() || value.Exponent() < -30 || value.Exponent() > 18 {
		return decimal.Zero, unsafePrice("invalid or negative numeric price")
	}
	value = value.Shift(scale)
	// Billing persists at most 18 decimal places. Never round a source price.
	value, err = decimal.NewFromString(value.String())
	if err != nil || value.Exponent() < -18 || len(value.String()) > 64 {
		return decimal.Zero, unsafePrice("price exceeds billing precision")
	}
	return value, nil
}

func addRate(target rates, dimension string, raw common.RawMessage, scale int32) error {
	value, err := amount(raw, scale)
	if err != nil {
		return err
	}
	if previous, exists := target[dimension]; exists && !previous.Equal(value) {
		return unsafePrice("conflicting prices for " + dimension)
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

func (plan pricePlan) version(model string) (billing.PriceVersion, error) {
	if plan.usageScope == billing.UsageScopeEmbedding {
		if err := plan.normalizeEmbedding(); err != nil {
			return billing.PriceVersion{}, err
		}
	}
	base := plan.standard.base
	_, input := base["p"]
	_, output := base["c"]
	_, images := base["images"]
	if images && output {
		if len(plan.standard.tiers) > 0 || len(plan.services) > 0 {
			return billing.PriceVersion{}, unsafePrice("unsupported combined image-count and token tier schedule")
		}
		if rate, exists := base["img_o"]; exists && !rate.IsZero() {
			return billing.PriceVersion{}, unsafePrice("overlapping per-image and image-token output prices")
		}
		// The image count owns image output; exclude those tokens from the
		// residual text output line instead of charging both representations.
		base["img_o"] = decimal.Zero
	}
	if !input || (!output && plan.usageScope != billing.UsageScopeEmbedding) {
		if !(images && len(base) == 1 && len(plan.standard.tiers) == 0 && len(plan.services) == 0) {
			return billing.PriceVersion{}, missingPrice("missing explicit input/output price")
		}
	}
	version := billing.PriceVersion{Model: model, Mode: billing.ModeUnitPrice, UsageScope: plan.usageScope, Prices: append([]billing.UnitPrice(nil), plan.extras...)}
	if images {
		version.Prices = append(version.Prices, billing.UnitPrice{Quantity: "images", Unit: "per_image", Price: base["images"].String()})
		delete(base, "images")
	}
	if len(base) == 0 {
		return billing.PreparePriceVersion(version)
	}
	union := make(map[string]bool)
	schedules := []schedule{plan.standard}
	for _, s := range plan.services {
		schedules = append(schedules, s)
	}
	for _, s := range schedules {
		for dimension := range s.base {
			union[dimension] = true
		}
		for _, tier := range s.tiers {
			for dimension := range tier.rates {
				union[dimension] = true
			}
		}
	}
	standard, err := scheduleExpression(plan.standard, union)
	if err != nil {
		return version, err
	}
	branches := make(map[string]string, 2)
	for _, service := range []struct {
		name, multiplier string
	}{{"priority", "2"}, {"flex", "0.5"}} {
		branch := "(" + standard + ") * " + service.multiplier
		if explicit, exists := plan.services[service.name]; exists {
			branch, err = scheduleExpression(explicit, union)
			if err != nil {
				return version, err
			}
		}
		branches[service.name] = branch
	}
	// Freeze the service policy with the token schedule. Separate image/tool
	// lines do not participate, and explicit partial schedules inherit base rates.
	expression := "attr(\"service_tier\") == \"priority\" || attr(\"service_tier\") == \"fast\" ? (" + branches["priority"] + ") : " +
		"attr(\"service_tier\") == \"ultrafast\" ? ((" + standard + ") * 2) : " +
		"attr(\"service_tier\") == \"flex\" ? (" + branches["flex"] + ") : (" + standard + ")"
	version.Mode = billing.ModeExpression
	version.Expression = "v1:" + expression
	return billing.PreparePriceVersion(version)
}

func (plan pricePlan) normalizeEmbedding() error {
	if len(plan.extras) != 0 {
		return unsafePrice("embedding price contains non-token charges")
	}
	schedules := []schedule{plan.standard}
	for _, service := range plan.services {
		schedules = append(schedules, service)
	}
	for _, s := range schedules {
		sets := []rates{s.base}
		for _, tier := range s.tiers {
			sets = append(sets, tier.rates)
		}
		for _, values := range sets {
			for _, dimension := range []string{"c", "img_o", "ao", "images"} {
				if value, exists := values[dimension]; exists {
					if !value.IsZero() {
						return unsafePrice("embedding price contains output charges")
					}
					delete(values, dimension)
				}
			}
		}
	}
	return nil
}

func scheduleExpression(s schedule, union map[string]bool) (string, error) {
	sort.Slice(s.tiers, func(i, j int) bool { return s.tiers[i].threshold < s.tiers[j].threshold })
	current := s.base
	branches := make([]string, 0, len(s.tiers)+1)
	base, err := ratesExpression(current, union)
	if err != nil {
		return "", err
	}
	branches = append(branches, base)
	for i, tier := range s.tiers {
		if tier.threshold <= 0 || (i > 0 && tier.threshold == s.tiers[i-1].threshold) {
			return "", unsafePrice("invalid or duplicate context threshold")
		}
		current = overlay(current, tier.rates)
		branch, err := ratesExpression(current, union)
		if err != nil {
			return "", err
		}
		branches = append(branches, branch)
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

func ratesExpression(values rates, union map[string]bool) (string, error) {
	var terms []string
	for _, dimension := range dimensionOrder {
		if !union[dimension] {
			continue
		}
		value, exists := values[dimension]
		if !exists {
			if dimension == "img_cr" {
				return "", unsafePrice("incomplete image-cache tier schedule")
			}
			// UsedVars spans every branch. A category absent in this branch must
			// retain its ordinary token rate after canonical normalization.
			fallback := "p"
			if dimension == "c" || dimension == "img_o" || dimension == "ao" {
				fallback = "c"
			}
			if dimension == "images" {
				return "", unsafePrice("incomplete per-image schedule")
			}
			value, exists = values[fallback]
			if !exists {
				return "", missingPrice("incomplete token schedule")
			}
		}
		if _, err := amount(common.RawMessage(value.String()), 0); err != nil {
			return "", err
		}
		if dimension == "images" {
			terms = append(terms, "image_count * fixed("+value.String()+")")
		} else {
			terms = append(terms, dimension+" * "+value.String())
		}
	}
	if len(terms) == 0 {
		return "", missingPrice("missing prices")
	}
	return strings.Join(terms, " + "), nil
}

func rejectUnknownPrice(key string, raw common.RawMessage) error {
	value, err := amount(raw, 0)
	if err == nil && value.IsZero() {
		return nil
	}
	return unsafePrice(fmt.Sprintf("unsupported charge dimension %s", key))
}

func positiveThreshold(raw common.RawMessage) (int64, error) {
	value, err := amount(raw, 0)
	if err != nil || !value.IsInteger() || !value.IsPositive() || value.GreaterThan(decimal.NewFromInt(1_000_000_000)) {
		return 0, unsafePrice("invalid context threshold")
	}
	return value.IntPart(), nil
}
