package autopricing

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
)

// AxonHub publishes a provider catalog. Qualified identities stay separate;
// the catalog assembler decides whether a bare alias is unambiguous.
func axonHubCatalog(root rawObject) ([]catalogRecord, error) {
	providers := root
	if raw, exists := root["providers"]; exists {
		var err error
		providers, err = object(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid axonhub providers: %w", err)
		}
	}
	var records []catalogRecord
	for _, provider := range keys(providers) {
		if provider == "updated_at" {
			continue
		}
		if provider == "" || strings.TrimSpace(provider) != provider {
			return nil, fmt.Errorf("invalid axonhub provider identity")
		}
		raw := bytes.TrimSpace(providers[provider])
		if len(raw) == 0 || raw[0] != '[' {
			item, err := object(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid axonhub provider %s: %w", provider, err)
			}
			raw = bytes.TrimSpace(item["models"])
		}
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return nil, fmt.Errorf("missing axonhub models for %s", provider)
		}
		if raw[0] == '[' {
			var models []common.RawMessage
			if err := common.Unmarshal(raw, &models); err != nil {
				return nil, fmt.Errorf("invalid axonhub models array for %s", provider)
			}
			for _, value := range models {
				item, err := object(value)
				if err != nil || rawString(item["id"]) == "" {
					return nil, fmt.Errorf("unidentified axonhub model for %s", provider)
				}
				records = append(records, axonHubRecord(provider, rawString(item["id"]), item, nil))
			}
			continue
		}
		models, err := object(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid axonhub models for %s: %w", provider, err)
		}
		for _, key := range keys(models) {
			if key == "" || strings.TrimSpace(key) != key {
				return nil, fmt.Errorf("invalid axonhub model identity for %s", provider)
			}
			item, itemErr := object(models[key])
			id := rawString(item["id"])
			if id == "" {
				id = key
				if _, explicit := item["id"]; explicit && itemErr == nil {
					itemErr = unsafePrice("invalid model id")
				}
			}
			records = append(records, axonHubRecord(provider, id, item, itemErr))
		}
	}
	return records, nil
}

func axonHubRecord(provider, id string, item rawObject, err error) catalogRecord {
	qualified := id
	if !strings.HasPrefix(id, provider+"/") {
		qualified = provider + "/" + id
	}
	record := catalogRecord{key: qualified, model: id, alias: id, err: err}
	if err == nil {
		record.plan, record.err = axonHubPlan(item)
	}
	return record
}

var axonDimensions = map[string]string{"input": "p", "output": "c", "cache_read": "cr", "cacheRead": "cr", "cache_write": "cc", "cacheWrite": "cc", "input_audio": "ai", "output_audio": "ao"}

func axonHubPlan(item rawObject) (pricePlan, error) {
	plan := pricePlan{standard: schedule{base: make(rates)}}
	if rawString(item["type"]) == "embedding" {
		plan.usageScope = billing.UsageScopeEmbedding
	}
	raw, exists := item["cost"]
	if !exists || string(raw) == "null" {
		return plan, missingPrice("missing cost")
	}
	cost, err := object(raw)
	if err != nil {
		return plan, err
	}
	plan.standard.base, err = axonRates(cost)
	if err != nil {
		return plan, err
	}
	var legacyTier rates
	if raw, exists := cost["context_over_200k"]; exists {
		tier, err := object(raw)
		if err != nil {
			return plan, err
		}
		values, err := axonRates(tier)
		if err != nil {
			return plan, err
		}
		legacyTier = values
	}
	if raw, exists := cost["tiers"]; exists {
		var tiers []rawObject
		if err := common.Unmarshal(raw, &tiers); err != nil {
			return plan, unsafePrice("invalid context tiers")
		}
		for _, tier := range tiers {
			selector, err := object(tier["tier"])
			if err != nil || rawString(selector["type"]) != "context" {
				return plan, unsafePrice("unsupported tier selector")
			}
			threshold, err := positiveThreshold(selector["size"])
			if err != nil {
				return plan, err
			}
			values, err := axonRates(tier)
			if err != nil {
				return plan, err
			}
			plan.standard.tiers = append(plan.standard.tiers, priceTier{threshold: threshold, rates: values})
		}
	}
	if legacyTier != nil {
		if len(plan.standard.tiers) == 0 {
			plan.standard.tiers = append(plan.standard.tiers, priceTier{threshold: 200000, rates: legacyTier})
		} else {
			// The legacy field is also emitted for thresholds other than 200k.
			// Trust the typed threshold only when its prices prove equivalence.
			matched := false
			for _, tier := range plan.standard.tiers {
				if equalRates(tier.rates, legacyTier) {
					matched = true
					break
				}
			}
			if !matched {
				return plan, unsafePrice("conflicting context tier formats")
			}
		}
	}
	return plan, nil
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

func axonRates(cost rawObject) (rates, error) {
	values := make(rates)
	for _, key := range keys(cost) {
		if key == "context_over_200k" || key == "tiers" || key == "tier" || key == "reasoning" {
			continue
		}
		dimension, known := axonDimensions[key]
		if !known {
			if err := rejectUnknownPrice(key, cost[key]); err != nil {
				return nil, err
			}
			continue
		}
		if err := addRate(values, dimension, cost[key], 0); err != nil {
			return nil, err
		}
	}
	if raw, exists := cost["reasoning"]; exists {
		value, err := amount(raw, 0)
		if err != nil {
			return nil, err
		}
		if output, exists := values["c"]; !exists || !value.Equal(output) {
			return nil, unsafePrice("unsupported separate reasoning price")
		}
	}
	return values, nil
}
