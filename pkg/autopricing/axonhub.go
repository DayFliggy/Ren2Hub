package autopricing

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
)

// AxonHub publishes a provider catalog. Qualified identities stay separate;
// the catalog assembler decides whether a bare alias is unambiguous.
func axonHubCatalog(root rawObject) ([]catalogRecord, error) {
	providers := root
	rootPath := ""
	if raw, exists := root["providers"]; exists {
		var err error
		providers, err = object(raw)
		rootPath = "/providers"
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
		modelPath := rootPath + "/" + jsonPointerToken(provider)
		if len(raw) == 0 || raw[0] != '[' {
			item, err := object(raw)
			if err != nil {
				return nil, fmt.Errorf("invalid axonhub provider %s: %w", provider, err)
			}
			raw = bytes.TrimSpace(item["models"])
			modelPath += "/models"
		}
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return nil, fmt.Errorf("missing axonhub models for %s", provider)
		}
		if raw[0] == '[' {
			var models []common.RawMessage
			if err := common.Unmarshal(raw, &models); err != nil {
				return nil, fmt.Errorf("invalid axonhub models array for %s", provider)
			}
			for index, value := range models {
				item, err := object(value)
				if err != nil || rawString(item["id"]) == "" {
					return nil, fmt.Errorf("unidentified axonhub model for %s", provider)
				}
				records = append(records, axonHubRecord(provider, rawString(item["id"]), item, nil, modelPath+"/"+strconv.Itoa(index)))
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
			records = append(records, axonHubRecord(provider, id, item, itemErr, modelPath+"/"+jsonPointerToken(key)))
		}
	}
	return records, nil
}

func axonHubRecord(provider, id string, item rawObject, err error, path string) catalogRecord {
	qualified := id
	if !strings.HasPrefix(id, provider+"/") {
		qualified = provider + "/" + id
	}
	record := catalogRecord{key: qualified, model: id, alias: id, provider: provider, err: err}
	if err == nil {
		record.plan, record.err = axonHubPlan(item)
		for i := range record.plan.evidence {
			record.plan.evidence[i].SourcePath = path + record.plan.evidence[i].SourcePath
		}
	}
	return record
}

var axonDimensions = map[string]string{"input": "p", "output": "c", "cache_read": "cr", "cacheRead": "cr", "cache_write": "cc", "cacheWrite": "cc", "input_audio": "ai", "cache_read_audio": "ai_cr", "output_audio": "ao", "reasoning": "reasoning"}

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
	plan.evidence = append(plan.evidence, axonEvidence(cost, "/cost", "default")...)
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
		plan.evidence = append(plan.evidence, axonEvidence(tier, "/cost/context_over_200k", "context")...)
	}
	if raw, exists := cost["tiers"]; exists {
		var tiers []rawObject
		if err := common.Unmarshal(raw, &tiers); err != nil {
			return plan, unsafePrice("invalid context tiers")
		}
		for index, tier := range tiers {
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
			path := "/cost/tiers/" + strconv.Itoa(index)
			plan.evidence = append(plan.evidence, axonEvidence(tier, path, "context")...)
			plan.evidence = append(plan.evidence, billing.PriceFieldEvidence{Target: "context_threshold", SourcePath: path + "/tier/size", RawValue: string(selector["size"]), SourceUnit: "tokens", Conversion: "single_context_threshold"})
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
		if key == "context_over_200k" || key == "tiers" || key == "tier" {
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
	return values, nil
}

func axonEvidence(cost rawObject, path, target string) []billing.PriceFieldEvidence {
	var fields []billing.PriceFieldEvidence
	for _, key := range keys(cost) {
		if dimension, known := axonDimensions[key]; known {
			fields = append(fields, billing.PriceFieldEvidence{Target: target + "." + dimension, SourcePath: path + "/" + jsonPointerToken(key), RawValue: string(cost[key]), SourceUnit: "usd_per_million_tokens", Conversion: "identity"})
		}
	}
	return fields
}
