package operation_setting

import (
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// ---------------------------------------------------------------------------
// Tool call prices ($/1K calls, admin-configurable)
// DB key: tool_price_setting.prices
//
// Key format:
//   - "tool_name"              → default price for all models
//   - "tool_name:model_prefix*" → override for models matching the prefix
//
// Only explicitly saved values are indexed. Longest model prefixes take
// precedence over the tool default, including explicitly saved zero.
// ---------------------------------------------------------------------------

const ToolPriceOptionKey = "tool_price_setting.prices"

// ToolPriceSetting is managed by config.GlobalConfig.Register.
// Prices starts empty; missing prices remain unconfigured.
type ToolPriceSetting struct {
	Prices map[string]float64 `json:"prices"`
}

var toolPriceSetting = ToolPriceSetting{
	Prices: make(map[string]float64),
}

// builtInToolNames are the hosted-tool price keys NewAPI's built-in price
// seed covers. Ren2Hub seeds no prices, so the set is a static list of the
// same names, never derived from operator configuration.
var builtInToolNames = map[string]struct{}{
	"web_search":                    {},
	"web_search_preview":            {},
	"file_search":                   {},
	"google_search":                 {},
	"google_search_grounded_prompt": {},
	"image_generation":              {},
	"search_std":                    {},
	"search_pro":                    {},
	"search_pro_sogou":              {},
	"search_pro_quark":              {},
	"search_strategy_turbo":         {},
	"search_strategy_max":           {},
	"search_strategy_agent":         {},
	"search_strategy_agent_max":     {},
	"bing_web_search":               {},
	"x_search_posts":                {},
	"x_search_profiles":             {},
}

func init() {
	config.GlobalConfig.Register("tool_price_setting", &toolPriceSetting)
	RebuildToolPriceIndex()
}

// IsBuiltInToolPriceKey reports whether name is a built-in hosted-tool price
// key. Operator prices never count.
func IsBuiltInToolPriceKey(name string) bool {
	_, ok := builtInToolNames[name]
	return ok
}

// ---------------------------------------------------------------------------
// Precomputed price index (atomic, lock-free on read path)
// ---------------------------------------------------------------------------

type prefixEntry struct {
	prefix string
	price  float64
}

type toolPriceIndex struct {
	defaults map[string]float64
	prefixes map[string][]prefixEntry
}

var currentIndex atomic.Pointer[toolPriceIndex]

func isValidToolPrice(price float64) bool {
	return price >= 0 && !math.IsNaN(price) && !math.IsInf(price, 0)
}

func decodeToolPricesJSON(value string) (map[string]float64, error) {
	rawValue := json.RawMessage(strings.TrimSpace(value))
	if common.GetJsonType(rawValue) != "object" {
		return nil, common.NewMessage("Tool prices must be a JSON object")
	}

	var rawPrices map[string]json.RawMessage
	if err := common.Unmarshal(rawValue, &rawPrices); err != nil {
		return nil, common.NewMessage("Failed to parse tool prices: {{error}}", map[string]any{"error": err.Error()})
	}

	prices := make(map[string]float64, len(rawPrices))
	for name, rawPrice := range rawPrices {
		var entryErr error
		if common.GetJsonType(rawPrice) != "number" {
			entryErr = common.NewMessage(`Tool price "{{name}}" must be a non-negative number`, map[string]any{"name": name})
		} else {
			var price float64
			if err := common.Unmarshal(rawPrice, &price); err != nil {
				entryErr = common.NewMessage(`Failed to parse tool price "{{name}}": {{error}}`, map[string]any{"name": name, "error": err.Error()})
			} else if !isValidToolPrice(price) {
				entryErr = common.NewMessage(`Tool price "{{name}}" must be a finite non-negative number`, map[string]any{"name": name})
			} else {
				prices[name] = price
			}
		}

		if entryErr == nil {
			continue
		}
		return nil, entryErr
	}
	return prices, nil
}

// ValidateToolPricesJSON validates an operator-supplied complete price map.
// A numeric zero is valid and intentionally disables the matching rule.
func ValidateToolPricesJSON(value string) error {
	_, err := decodeToolPricesJSON(value)
	return err
}

func LoadToolPricesFromJSONString(value string) error {
	prices, err := decodeToolPricesJSON(value)
	if err != nil {
		return err
	}
	toolPriceSetting.Prices = prices
	RebuildToolPriceIndex()
	return nil
}

// RebuildToolPriceIndex rebuilds the lookup index from the current config.
// Called on init and after config updates. Not on the billing hot path.
func RebuildToolPriceIndex() {
	merged := make(map[string]float64, len(toolPriceSetting.Prices))
	for k, v := range toolPriceSetting.Prices {
		if !isValidToolPrice(v) {
			continue
		}
		merged[k] = v
	}

	idx := &toolPriceIndex{
		defaults: make(map[string]float64),
		prefixes: make(map[string][]prefixEntry),
	}

	for key, price := range merged {
		before, after, ok := strings.Cut(key, ":")
		if !ok {
			idx.defaults[key] = price
			continue
		}
		toolName := before
		modelPart := after
		prefix := strings.TrimSuffix(modelPart, "*")
		idx.prefixes[toolName] = append(idx.prefixes[toolName], prefixEntry{prefix: prefix, price: price})
	}

	for tool := range idx.prefixes {
		entries := idx.prefixes[tool]
		sort.Slice(entries, func(i, j int) bool {
			if len(entries[i].prefix) == len(entries[j].prefix) {
				return entries[i].prefix < entries[j].prefix
			}
			return len(entries[i].prefix) > len(entries[j].prefix)
		})
		idx.prefixes[tool] = entries
	}

	currentIndex.Store(idx)
}

// ModelToolPrices returns only explicitly configured prices in USD/1K calls.
func ModelToolPrices(modelName string) map[string]float64 {
	idx := currentIndex.Load()
	prices := map[string]float64{}
	if idx == nil {
		return prices
	}
	for name, price := range idx.defaults {
		prices[name] = price
	}
	for name, entries := range idx.prefixes {
		for _, entry := range entries {
			if strings.HasPrefix(modelName, entry.prefix) {
				prices[name] = entry.price
				break
			}
		}
	}
	return prices
}
