package pricesource

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var catalogDimensions = map[string]string{
	"input": "p", "output": "c", "cache_read": "cr", "cacheRead": "cr",
	"cache_write": "cc", "cacheWrite": "cc", "input_audio": "ai", "output_audio": "ao", "reasoning": "reasoning",
}

func parseProviderCatalog(id string, root object) ([]Model, error) {
	if id == "axonhub" {
		var err error
		root, err = readObject(root["providers"])
		if err != nil {
			return nil, fmt.Errorf("missing providers object")
		}
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

var upstreamFields = map[string]string{
	"model_price": "ModelPrice", "model_ratio": "ModelRatio", "completion_ratio": "CompletionRatio",
	"cache_ratio": "CacheRatio", "create_cache_ratio": "CreateCacheRatio", "image_ratio": "ImageRatio",
	"audio_ratio": "AudioRatio", "audio_completion_ratio": "AudioCompletionRatio",
	"billing_mode": "billing_setting.billing_mode", "billing_expr": "billing_setting.billing_expr",
}

func parseBasellm(root object) ([]Model, error) {
	var success bool
	if common.Unmarshal(root["success"], &success) != nil || !success {
		return nil, fmt.Errorf("source did not report success")
	}
	data, err := readObject(root["data"])
	if err != nil {
		return nil, err
	}
	byModel := make(map[string]*Model)
	for _, key := range keys(data) {
		field, supported := upstreamFields[key]
		if !supported {
			return nil, fmt.Errorf("unsupported pricing field: %s", key)
		}
		values, err := readObject(data[key])
		if err != nil {
			return nil, fmt.Errorf("invalid %s price map", key)
		}
		for _, name := range keys(values) {
			model := byModel[name]
			if model == nil {
				model = &Model{ModelName: name, Provider: "", Pricing: map[string]any{}}
				byModel[name] = model
			}
			if key == "billing_mode" || key == "billing_expr" {
				value := stringValue(values[name])
				if value == "" || (key == "billing_mode" && value != "ratio" && value != "tiered_expr") {
					model.UnsupportedReason = "invalid billing mode or expression"
					continue
				}
				model.Pricing[field] = value
				continue
			}
			value, err := price(values[name], 0)
			if err != nil {
				model.UnsupportedReason = err.Error()
				continue
			}
			// Drafts cross a JavaScript JSON boundary before they are saved.
			// A numeric JSON literal would lose precision in the browser.
			model.Pricing[field] = value.String()
		}
	}
	result := make([]Model, 0, len(byModel))
	for _, model := range byModel {
		if model.Pricing["billing_setting.billing_mode"] == "tiered_expr" {
			expression, ok := model.Pricing["billing_setting.billing_expr"].(string)
			if !ok || strings.TrimSpace(expression) == "" {
				model.UnsupportedReason = "missing price expression"
			} else {
				model.Pricing = expressionPricing(expression)
			}
		} else if _, fixed := model.Pricing["ModelPrice"]; fixed {
			model.Pricing = map[string]any{"ModelPrice": model.Pricing["ModelPrice"], "billing_setting.billing_mode": "ratio"}
		} else if _, input := model.Pricing["ModelRatio"]; !input {
			model.UnsupportedReason = "missing model input price"
		} else {
			delete(model.Pricing, "billing_setting.billing_expr")
			model.Pricing["billing_setting.billing_mode"] = "ratio"
		}
		result = append(result, *model)
	}
	return result, nil
}
