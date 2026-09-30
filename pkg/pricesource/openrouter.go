package pricesource

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var openRouterDimensions = map[string]string{
	"prompt": "p", "completion": "c", "input_cache_read": "cr", "input_cache_write": "cc",
	"input_cache_write_1h": "cc1h", "input_audio": "ai", "output_audio": "ao", "internal_reasoning": "reasoning",
}

func parseOpenRouter(root object) ([]Model, error) {
	var entries []object
	if err := common.Unmarshal(root["data"], &entries); err != nil {
		return nil, fmt.Errorf("missing OpenRouter models array")
	}
	result := make([]Model, 0, len(entries))
	for _, entry := range entries {
		name := stringValue(entry["id"])
		provider, _, qualified := strings.Cut(name, "/")
		if !qualified {
			provider = ""
		}
		model := Model{ModelName: name, Provider: provider}
		pricing, err := readObject(entry["pricing"])
		if err == nil {
			var expression string
			expression, model.Warnings, err = openRouterExpression(pricing)
			if err == nil {
				model.Pricing = expressionPricing(expression)
			}
		}
		if err != nil {
			model.UnsupportedReason = err.Error()
		}
		result = append(result, model)
	}
	return result, nil
}

func openRouterRates(pricing object, structuralFields ...string) (rates, []string, error) {
	values := make(rates)
	var warnings []string
	for _, key := range keys(pricing) {
		if slices.Contains(structuralFields, key) {
			continue
		}
		if key == "web_search" {
			if !zeroToolPrice(pricing[key], 0) {
				return nil, nil, fmt.Errorf("%s has a non-zero or invalid tool fee that cannot be imported", key)
			}
			warnings = append(warnings, key+" is explicitly zero; tool pricing is not synchronized")
			continue
		}
		dimension, known := openRouterDimensions[key]
		if !known {
			// These are per-request/per-image amounts, never token prices.
			if key == "request" || key == "image" {
				value, err := price(pricing[key], 0)
				if err != nil {
					return nil, nil, err
				}
				if value.IsZero() {
					continue
				}
				values[key] = value
				continue
			}
			return nil, nil, fmt.Errorf("unsupported OpenRouter price field: %s", key)
		}
		if err := addRate(values, dimension, pricing[key], 6); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	return values, warnings, nil
}

func openRouterExpression(pricing object) (string, []string, error) {
	base, warnings, err := openRouterRates(pricing, "overrides")
	if err != nil {
		return "", nil, err
	}
	for _, unit := range []string{"request", "image"} {
		value, present := base[unit]
		if !present {
			continue
		}
		for key, other := range base {
			if key != unit && !other.IsZero() {
				return "", warnings, fmt.Errorf("combined token and %s charges cannot be represented", unit)
			}
		}
		if _, exists := pricing["overrides"]; exists {
			return "", warnings, fmt.Errorf("conditional %s charges cannot be represented", unit)
		}
		if unit == "image" {
			return "tier(\"images\", fixed(" + value.String() + ")) * image_count", warnings, nil
		}
		return "tier(\"request\", fixed(" + value.String() + "))", warnings, nil
	}
	s := schedule{base: base}
	if raw, exists := pricing["overrides"]; exists {
		var tiers []object
		if err := common.Unmarshal(raw, &tiers); err != nil || len(tiers) == 0 {
			return "", warnings, fmt.Errorf("invalid OpenRouter price overrides")
		}
		for _, tier := range tiers {
			if _, exists := tier["min_prompt_tokens"]; !exists {
				return "", warnings, fmt.Errorf("unsupported OpenRouter price override selector")
			}
			boundary, err := threshold(tier["min_prompt_tokens"])
			if err != nil {
				return "", warnings, err
			}
			values, extraWarnings, err := openRouterRates(tier, "min_prompt_tokens")
			if err != nil {
				return "", warnings, err
			}
			warnings = append(warnings, extraWarnings...)
			if _, exists := values["request"]; exists {
				return "", warnings, fmt.Errorf("request surcharge in token tier")
			}
			if _, exists := values["image"]; exists {
				return "", warnings, fmt.Errorf("image surcharge in token tier")
			}
			s.tiers = append(s.tiers, priceTier{threshold: boundary, inclusive: true, prices: values})
		}
	}
	expression, err := scheduleExpression(s, false)
	return expression, warnings, err
}
