package pricesource

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/common"
)

var catalogDimensions = map[string]string{
	"input": "p", "output": "c", "cache_read": "cr", "cacheRead": "cr",
	"cache_write": "cc", "cacheWrite": "cc", "input_audio": "ai", "output_audio": "ao", "reasoning": "reasoning",
}

// AxonHub 使用 PublicProviderConf 的 all.json：providers.<provider>.models，
// models 可以是数组或以模型名为键的对象。
func parseAxonHub(document object) ([]Model, error) {
	root, err := readObject(document["providers"])
	if err != nil {
		return nil, fmt.Errorf("missing providers object")
	}
	var result []Model
	for _, provider := range keys(root) {
		data, err := readObject(root[provider])
		if err != nil {
			return nil, fmt.Errorf("invalid provider: %s", provider)
		}
		raw := bytes.TrimSpace(data["models"])
		var entries []object
		if len(raw) > 0 && raw[0] == '[' {
			if err := common.Unmarshal(raw, &entries); err != nil {
				return nil, fmt.Errorf("invalid models for %s", provider)
			}
		} else {
			models, err := readObject(raw)
			if err != nil {
				return nil, fmt.Errorf("missing models for %s", provider)
			}
			for _, key := range keys(models) {
				entry, err := readObject(models[key])
				if err != nil {
					return nil, fmt.Errorf("invalid model %s/%s", provider, key)
				}
				if _, exists := entry["id"]; !exists {
					encoded, _ := common.Marshal(key)
					entry["id"] = encoded
				}
				entries = append(entries, entry)
			}
		}
		for _, entry := range entries {
			model := Model{ModelName: stringValue(entry["id"]), Provider: provider}
			cost, err := readObject(entry["cost"])
			if err != nil {
				err = fmt.Errorf("model has no valid price data")
			}
			if err == nil {
				var expression string
				expression, err = catalogExpression(cost, stringValue(entry["type"]) == "embedding")
				if err == nil {
					model.Pricing = expressionPricing(expression)
				}
			}
			if err != nil {
				model.UnsupportedReason = err.Error()
			}
			result = append(result, model)
		}
	}
	return result, nil
}

func catalogRates(cost object, structuralFields ...string) (rates, error) {
	result := make(rates)
	for _, key := range keys(cost) {
		if slices.Contains(structuralFields, key) {
			continue
		}
		dimension, exists := catalogDimensions[key]
		if !exists {
			return nil, fmt.Errorf("unsupported price field: %s", key)
		}
		if err := addRate(result, dimension, cost[key], 0); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
	}
	return result, nil
}

func catalogExpression(cost object, embedding bool) (string, error) {
	base, err := catalogRates(cost, "tiers", "context_over_200k")
	if err != nil {
		return "", err
	}
	s := schedule{base: base}
	if raw, exists := cost["tiers"]; exists {
		var tiers []object
		if err := common.Unmarshal(raw, &tiers); err != nil || len(tiers) == 0 {
			return "", fmt.Errorf("invalid context tiers")
		}
		for _, tier := range tiers {
			selector, err := readObject(tier["tier"])
			if err != nil || len(selector) != 2 || stringValue(selector["type"]) != "context" {
				return "", fmt.Errorf("unsupported price tier selector")
			}
			boundary, err := threshold(selector["size"])
			if err != nil {
				return "", err
			}
			values, err := catalogRates(tier, "tier")
			if err != nil {
				return "", err
			}
			s.tiers = append(s.tiers, priceTier{threshold: boundary, prices: values})
		}
	}
	if raw, exists := cost["context_over_200k"]; exists {
		legacy, err := readObject(raw)
		if err != nil {
			return "", err
		}
		values, err := catalogRates(legacy)
		if err != nil {
			return "", err
		}
		if len(s.tiers) == 0 {
			s.tiers = append(s.tiers, priceTier{threshold: 200000, prices: values})
		} else {
			matched := false
			for _, tier := range s.tiers {
				if tier.threshold == 200000 && equalRates(tier.prices, values) {
					matched = true
				}
			}
			if !matched {
				return "", fmt.Errorf("conflicting legacy and typed context tiers")
			}
		}
	}
	return scheduleExpression(s, embedding)
}
