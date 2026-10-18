// Package pricesource turns public price documents into editable NewAPI drafts.
// Fetching and parsing never publish prices or write application state.
package pricesource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

type Source struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"-"`
}

type Model struct {
	ModelName         string         `json:"model_name"`
	Provider          string         `json:"provider"`
	Pricing           map[string]any `json:"pricing"`
	UnsupportedReason string         `json:"unsupported_reason,omitempty"`
	Warnings          []string       `json:"warnings,omitempty"`
}

func Sources() []Source {
	return []Source{
		{"sub2api", "Sub2API", "https://raw.githubusercontent.com/Wei-Shaw/model-price-repo/main/model_prices_and_context_window.json"},
		{"axonhub", "AxonHub", "https://raw.githubusercontent.com/ThinkInAIXYZ/PublicProviderConf/refs/heads/dev/dist/all.json"},
		{"basellm", "basellm", "https://basellm.github.io/llm-metadata/api/newapi/ratio_config-v1-base.json"},
		{"models_dev", "models.dev", "https://models.dev/api.json"},
		{"openrouter", "OpenRouter", "https://openrouter.ai/api/v1/models"},
	}
}

func Select(ids []string) ([]Source, error) {
	all := Sources()
	if len(ids) == 0 {
		return all, nil
	}
	byID := make(map[string]Source, len(all))
	for _, source := range all {
		byID[source.ID] = source
	}
	selected := make([]Source, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		source, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown price source: %s", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate price source: %s", id)
		}
		seen[id] = true
		selected = append(selected, source)
	}
	return selected, nil
}

type object map[string]common.RawMessage

// Duplicate provider, model, or price keys must not silently select the last
// value supplied by an external document. Raw values retain exact decimals.
func (value *object) UnmarshalJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return fmt.Errorf("expected JSON object")
	}
	result := make(object)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid object key")
		}
		if _, exists := result[key]; exists {
			return fmt.Errorf("duplicate source object key: %s", key)
		}
		var raw common.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		result[key] = raw
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	*value = result
	return nil
}

func readObject(raw []byte) (object, error) {
	var value object
	if err := common.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("expected JSON object")
	}
	return value, nil
}

func stringValue(raw common.RawMessage) string {
	var value string
	if common.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func keys(value object) []string {
	result := make([]string, 0, len(value))
	for key := range value {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func Parse(id string, document []byte) ([]Model, error) {
	root, err := readObject(document)
	if err != nil {
		return nil, err
	}
	var models []Model
	switch id {
	case "sub2api":
		models, err = parseSub2API(root)
	case "axonhub", "models_dev":
		models, err = parseProviderCatalog(id, root)
	case "basellm":
		models, err = parseBasellm(root)
	case "openrouter":
		models, err = parseOpenRouter(root)
	default:
		return nil, fmt.Errorf("unknown price source: %s", id)
	}
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("source contains no model prices")
	}
	if len(models) > 30000 {
		return nil, fmt.Errorf("source exceeds 30000 model entries")
	}
	seen := make(map[string]bool, len(models))
	for i := range models {
		entry := &models[i]
		if entry.Pricing == nil {
			entry.Pricing = map[string]any{}
		}
		identity := entry.Provider + "\x00" + entry.ModelName
		if entry.ModelName == "" || len(entry.ModelName) > 300 || strings.TrimSpace(entry.ModelName) != entry.ModelName || strings.ContainsAny(entry.ModelName, "\x00\r\n") {
			return nil, fmt.Errorf("invalid model identity")
		}
		if seen[identity] {
			return nil, fmt.Errorf("duplicate model identity: %s", entry.ModelName)
		}
		seen[identity] = true
		if expression, ok := entry.Pricing["billing_setting.billing_expr"].(string); ok && entry.UnsupportedReason == "" {
			if len(expression) > 16384 {
				entry.UnsupportedReason = "price expression exceeds 16384 characters"
			} else if _, err := billingexpr.CompileFromCache(expression); err != nil {
				entry.UnsupportedReason = "price expression is not supported: " + err.Error()
			}
		}
		if entry.UnsupportedReason != "" {
			entry.Pricing = map[string]any{}
		}
		entry.Warnings = uniqueWarnings(entry.Warnings)
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].ModelName != models[j].ModelName {
			return models[i].ModelName < models[j].ModelName
		}
		return models[i].Provider < models[j].Provider
	})
	return models, nil
}

func expressionPricing(expression string) map[string]any {
	return map[string]any{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}
}
