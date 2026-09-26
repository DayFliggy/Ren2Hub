package autopricing

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/shopspring/decimal"
)

// Sub2API is a model-keyed LiteLLM catalog. Prices use USD/token;
// normalization happens here before the common PriceVersion conversion.
func sub2APICatalog(root rawObject) ([]catalogRecord, error) {
	records := make([]catalogRecord, 0, len(root))
	for _, id := range keys(root) {
		if id == "sample_spec" {
			continue
		}
		if id == "" || strings.TrimSpace(id) != id {
			return nil, fmt.Errorf("invalid sub2api model identity")
		}
		item, err := object(root[id])
		record := catalogRecord{key: id, model: id, err: err}
		if err == nil {
			record.plan, record.err = sub2APIPlan(item)
		}
		records = append(records, record)
	}
	return records, nil
}

var subDimensions = map[string]string{
	"input_cost_per_token": "p", "output_cost_per_token": "c",
	"cache_read_input_token_cost": "cr", "input_cost_per_token_cache_hit": "cr",
	"cache_creation_input_token_cost": "cc", "cache_creation_input_token_cost_above_1hr": "cc1h",
	"input_cost_per_image_token": "img", "output_cost_per_image_token": "img_o", "cache_read_input_image_token_cost": "img_cr",
	"input_cost_per_audio_token": "ai", "output_cost_per_audio_token": "ao", "output_cost_per_image": "images",
}
var contextSuffix = regexp.MustCompile(`_above_([0-9]+)k_tokens$`)

func sub2APIPlan(item rawObject) (pricePlan, error) {
	plan := pricePlan{standard: schedule{base: make(rates)}, services: make(map[string]schedule)}
	if rawString(item["mode"]) == "embedding" {
		plan.usageScope = billing.UsageScopeEmbedding
	}
	patches := map[string]map[int64]rates{"": {0: plan.standard.base}}
	for _, key := range keys(item) {
		if strings.HasSuffix(key, "_batches") {
			continue
		} // Batch APIs are a separate product, never a service_tier.
		if key == "tiered_pricing" || key == "long_context_input_token_threshold" || key == "long_context_threshold_inclusive" || key == "long_context_input_cost_multiplier" || key == "long_context_output_cost_multiplier" || key == "long_context_cache_read_cost_multiplier" {
			continue
		}
		if key == "output_cost_per_reasoning_token" || key == "search_context_cost_per_query" {
			continue
		}
		baseKey := key
		service := ""
		for _, name := range []string{"priority", "flex"} {
			if strings.HasSuffix(baseKey, "_"+name) {
				service = name
				baseKey = strings.TrimSuffix(baseKey, "_"+name)
				break
			}
		}
		threshold := int64(0)
		if match := contextSuffix.FindStringSubmatch(baseKey); match != nil {
			parsed, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil || parsed <= 0 || parsed > 1_000_000 {
				return plan, unsafePrice("invalid context threshold")
			}
			threshold = parsed * 1000
			baseKey = strings.TrimSuffix(baseKey, match[0])
		}
		dimension, known := subDimensions[baseKey]
		if !known {
			if strings.Contains(key, "_cost_") || strings.HasSuffix(key, "_cost") {
				if err := rejectUnknownPrice(key, item[key]); err != nil {
					return plan, err
				}
			}
			continue
		}
		if patches[service] == nil {
			patches[service] = make(map[int64]rates)
		}
		if patches[service][threshold] == nil {
			patches[service][threshold] = make(rates)
		}
		scale := int32(6)
		if dimension == "images" {
			scale = 0
		}
		if err := addRate(patches[service][threshold], dimension, item[key], scale); err != nil {
			return plan, fmt.Errorf("%s: %w", key, err)
		}
	}
	if raw, exists := item["output_cost_per_reasoning_token"]; exists {
		value, err := amount(raw, 6)
		if err != nil {
			return plan, err
		}
		if output, exists := plan.standard.base["c"]; !exists || !value.Equal(output) {
			return plan, unsafePrice("unsupported separate reasoning price")
		}
	}
	if raw, exists := item["search_context_cost_per_query"]; exists {
		search, err := object(raw)
		if err != nil {
			return plan, err
		}
		price := ""
		for _, size := range []string{"search_context_size_low", "search_context_size_medium", "search_context_size_high"} {
			value, err := amount(search[size], 0)
			if err != nil {
				return plan, err
			}
			if price != "" && price != value.String() {
				return plan, unsafePrice("unsupported search context price tiers")
			}
			price = value.String()
		}
		if len(search) != 3 {
			return plan, unsafePrice("unsupported search context price fields")
		}
		provider := rawString(item["litellm_provider"])
		names := []string{"web_search", "web_search_preview"}
		if strings.HasPrefix(provider, "vertex_ai") || provider == "gemini" {
			names = []string{"google_search"}
		} else if provider != "openai" && provider != "anthropic" && provider != "azure" {
			return plan, unsafePrice("unsupported search tool provider")
		}
		for _, name := range names {
			plan.extras = append(plan.extras, billing.UnitPrice{Quantity: "tool:" + name, Unit: "per_call", Price: price})
		}
	}
	// Image generation can expose only an image-output price. Preserve that
	// explicit rate for any residual output rather than inventing a free rate.
	if rawString(item["mode"]) == "image_generation" {
		if _, exists := plan.standard.base["c"]; !exists {
			if value, exists := plan.standard.base["img_o"]; exists {
				plan.standard.base["c"] = value
			}
		}
	}
	for threshold, values := range patches[""] {
		if threshold > 0 {
			plan.standard.tiers = append(plan.standard.tiers, priceTier{threshold: threshold, rates: values})
		}
	}
	if raw, exists := item["tiered_pricing"]; exists {
		if len(plan.standard.tiers) > 0 {
			return plan, unsafePrice("conflicting context tier formats")
		}
		if err := subRangeTiers(&plan, item, raw); err != nil {
			return plan, err
		}
	} else if _, exists := item["long_context_input_token_threshold"]; exists {
		if len(plan.standard.tiers) > 0 {
			return plan, unsafePrice("conflicting context tier formats")
		}
		thresholdValue, err := amount(item["long_context_input_token_threshold"], 0)
		if err != nil {
			return plan, err
		}
		threshold := int64(0)
		if !thresholdValue.IsZero() {
			threshold, err = positiveThreshold(item["long_context_input_token_threshold"])
		}
		if err != nil {
			return plan, err
		}
		values := make(rates)
		for key, dimension := range map[string]string{"long_context_input_cost_multiplier": "p", "long_context_output_cost_multiplier": "c", "long_context_cache_read_cost_multiplier": "cr"} {
			if raw, exists := item[key]; exists {
				multiplier, err := amount(raw, 0)
				if err != nil {
					return plan, err
				}
				base, exists := plan.standard.base[dimension]
				if !exists {
					return plan, missingPrice("long context multiplier has no base price")
				}
				if multiplier.IsZero() {
					continue
				} // Source uses zero to disable this surcharge.
				values[dimension] = base.Mul(multiplier)
				if dimension == "p" {
					for _, cache := range []string{"cr", "cc", "cc1h"} {
						if rate, exists := plan.standard.base[cache]; exists {
							if _, explicit := item["long_context_cache_read_cost_multiplier"]; cache != "cr" || !explicit {
								values[cache] = rate.Mul(multiplier)
							}
						}
					}
				}
			}
		}
		var inclusive bool
		if raw, exists := item["long_context_threshold_inclusive"]; exists {
			if err := common.Unmarshal(raw, &inclusive); err != nil {
				return plan, unsafePrice("invalid context boundary")
			}
		}
		if threshold > 0 {
			plan.standard.tiers = append(plan.standard.tiers, priceTier{threshold: threshold, inclusive: inclusive, rates: values})
		}
	}
	for i := range plan.standard.tiers {
		tier := &plan.standard.tiers[i]
		long := overlay(plan.standard.base, tier.rates)
		derived, err := serviceContextRates(plan.standard.base, long, plan.standard.base, tier.rates)
		if err != nil {
			return plan, err
		}
		tier.rates = overlay(derived, tier.rates)
	}
	for service, byThreshold := range patches {
		if service == "" {
			continue
		}
		basePatch := byThreshold[0]
		s := schedule{base: overlay(plan.standard.base, basePatch)}
		standardRates := plan.standard.base
		sort.Slice(plan.standard.tiers, func(i, j int) bool { return plan.standard.tiers[i].threshold < plan.standard.tiers[j].threshold })
		for _, tier := range plan.standard.tiers {
			standardRates = overlay(standardRates, tier.rates)
			// Sub2API applies service prices first, then the source's context
			// ratios. Explicit service/context absolute prices take precedence.
			values, err := serviceContextRates(plan.standard.base, standardRates, s.base, byThreshold[tier.threshold])
			if err != nil {
				return plan, err
			}
			values = overlay(values, byThreshold[tier.threshold])
			s.tiers = append(s.tiers, priceTier{threshold: tier.threshold, inclusive: tier.inclusive, rates: values})
			delete(byThreshold, tier.threshold)
		}
		for threshold, values := range byThreshold {
			if threshold > 0 {
				s.tiers = append(s.tiers, priceTier{threshold: threshold, rates: values})
			}
		}
		plan.services[service] = s
	}
	return plan, nil
}

func subRangeTiers(plan *pricePlan, item rawObject, raw common.RawMessage) error {
	var tiers []rawObject
	if err := common.Unmarshal(raw, &tiers); err != nil || len(tiers) == 0 {
		return unsafePrice("invalid tiered_pricing")
	}
	previousEnd := int64(0)
	var inclusive bool
	if value, exists := item["long_context_threshold_inclusive"]; exists {
		if err := common.Unmarshal(value, &inclusive); err != nil {
			return unsafePrice("invalid context boundary")
		}
	}
	for i, tier := range tiers {
		var bounds []common.RawMessage
		if err := common.Unmarshal(tier["range"], &bounds); err != nil || len(bounds) != 2 {
			return unsafePrice("invalid context range")
		}
		lower, err := amount(bounds[0], 0)
		if err != nil || !lower.Equal(decimal.NewFromInt(previousEnd)) {
			return unsafePrice("non-contiguous context ranges")
		}
		upper, err := positiveThreshold(bounds[1])
		if err != nil || upper <= previousEnd {
			return unsafePrice("invalid context range")
		}
		values := make(rates)
		for _, key := range keys(tier) {
			if key == "range" {
				continue
			}
			dimension, exists := subDimensions[key]
			if !exists {
				return unsafePrice("unsupported tier charge " + key)
			}
			scale := int32(6)
			if dimension == "images" {
				scale = 0
			}
			if err := addRate(values, dimension, tier[key], scale); err != nil {
				return err
			}
		}
		if _, exists := values["p"]; !exists {
			return missingPrice("missing tier input price")
		}
		if _, exists := values["c"]; !exists && plan.usageScope != billing.UsageScopeEmbedding {
			return missingPrice("missing tier output price")
		}
		if i == 0 {
			plan.standard.base = overlay(plan.standard.base, values)
		} else {
			plan.standard.tiers = append(plan.standard.tiers, priceTier{threshold: previousEnd, inclusive: inclusive, rates: values})
		}
		previousEnd = upper
	}
	return nil
}

func serviceContextRates(base, long, service, explicit rates) (rates, error) {
	result := make(rates)
	for dimension, value := range service {
		if price, exists := explicit[dimension]; exists {
			result[dimension] = price
			continue
		}
		if dimension == "images" {
			result[dimension] = value
			continue
		}
		ratioDimension := dimension
		if dimension == "cr" || dimension == "cc" || dimension == "cc1h" {
			ratioDimension = "p"
		}
		before, hasBase := base[ratioDimension]
		after, hasLong := long[ratioDimension]
		if !hasBase || !hasLong || before.Equal(after) {
			result[dimension] = value
			continue
		}
		if before.IsZero() {
			return nil, unsafePrice("cannot derive service tier from a zero base price")
		}
		product := value.Mul(after)
		converted, remainder := product.QuoRem(before, 18)
		if !remainder.Equal(decimal.Zero) {
			return nil, unsafePrice("service/context price exceeds billing precision")
		}
		result[dimension] = converted
	}
	return result, nil
}
