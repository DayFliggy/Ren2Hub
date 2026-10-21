package pricesource

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

var subDimensions = map[string]string{
	"input_cost_per_token": "p", "output_cost_per_token": "c",
	"cache_read_input_token_cost": "cr", "input_cost_per_token_cache_hit": "cr",
	"cache_creation_input_token_cost": "cc", "cache_creation_input_token_cost_above_1hr": "cc1h",
	"input_cost_per_image_token": "img", "output_cost_per_image_token": "img_o", "cache_read_input_image_token_cost": "img_cr",
	"input_cost_per_audio_token": "ai", "output_cost_per_audio_token": "ao", "output_cost_per_reasoning_token": "reasoning",
	"output_cost_per_image": "images",
}
var contextSuffix = regexp.MustCompile(`_above_([0-9]+)k_tokens$`)

// Sub2API 认识、但 NewAPI 计费表达式没有对应维度的计价字段。导入时跳过，
// 非零或无法解析时在草稿里警告：导入后的价格不包含这部分费用。
// 不在此表也不在 subDimensions 里的计价字段仍然整条拒收。
var subUnbilledFields = map[string]bool{
	"cache_storage_cost_per_token_per_hour": true,
}

func subUnbilledWarning(key string, raw common.RawMessage) string {
	if value, err := price(raw, 0); err == nil && value.IsZero() {
		return ""
	}
	return key + " is not billed locally; imported prices exclude it"
}

func parseSub2API(root object) ([]Model, error) {
	var result []Model
	for _, name := range keys(root) {
		if name == "sample_spec" {
			continue
		}
		model := Model{ModelName: name}
		entry, err := readObject(root[name])
		if err == nil {
			model.Provider = stringValue(entry["litellm_provider"])
			var expression string
			expression, model.Warnings, err = subExpression(entry)
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

func subExpression(entry object) (string, []string, error) {
	patches := map[string]map[int64]rates{"": {0: {}}}
	var warnings []string
	for _, key := range keys(entry) {
		if strings.HasSuffix(key, "_batches") {
			warnings = append(warnings, "batch product prices are not imported")
			continue
		}
		if strings.HasPrefix(key, "long_context_") || key == "tiered_pricing" {
			continue
		}
		if key == "search_context_cost_per_query" || key == "google_maps_grounding_cost_per_query" {
			if !zeroToolPrice(entry[key], 0) {
				return "", warnings, fmt.Errorf("%s has a non-zero or invalid tool fee that cannot be imported", key)
			}
			warnings = append(warnings, key+" is explicitly zero; tool pricing is not synchronized")
			continue
		}
		if subUnbilledFields[key] {
			if warning := subUnbilledWarning(key, entry[key]); warning != "" {
				warnings = append(warnings, warning)
			}
			continue
		}
		baseKey, service := key, ""
		for _, candidate := range []string{"priority", "flex"} {
			if strings.HasSuffix(baseKey, "_"+candidate) {
				service = candidate
				baseKey = strings.TrimSuffix(baseKey, "_"+candidate)
				break
			}
		}
		boundary := int64(0)
		if match := contextSuffix.FindStringSubmatch(baseKey); match != nil {
			parsed, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil || parsed <= 0 || parsed > 1000000 {
				return "", warnings, fmt.Errorf("invalid context threshold")
			}
			boundary = parsed * 1000
			baseKey = strings.TrimSuffix(baseKey, match[0])
		}
		dimension, known := subDimensions[baseKey]
		if !known {
			if strings.Contains(key, "_cost_") || strings.HasSuffix(key, "_cost") {
				return "", warnings, fmt.Errorf("unsupported charge field: %s", key)
			}
			continue
		}
		if patches[service] == nil {
			patches[service] = make(map[int64]rates)
		}
		if patches[service][boundary] == nil {
			patches[service][boundary] = make(rates)
		}
		scale := int32(6)
		if dimension == "images" {
			scale = 0
		}
		if err := addRate(patches[service][boundary], dimension, entry[key], scale); err != nil {
			return "", warnings, fmt.Errorf("%s: %w", key, err)
		}
	}
	s := schedule{base: patches[""][0]}
	for boundary, values := range patches[""] {
		if boundary > 0 {
			s.tiers = append(s.tiers, priceTier{threshold: boundary, prices: values})
		}
	}
	if err := subContextSchedule(entry, &s); err != nil {
		return "", warnings, err
	}
	if imagePrice, exists := s.base["images"]; exists {
		if len(s.base) != 1 || len(s.tiers) > 0 || len(patches) > 1 {
			return "", warnings, fmt.Errorf("combined image and token charges cannot be represented")
		}
		return "tier(\"images\", fixed(" + imagePrice.String() + ")) * image_count", warnings, nil
	}
	if stringValue(entry["mode"]) == "image_generation" {
		if _, exists := s.base["c"]; !exists {
			if value, ok := s.base["img_o"]; ok {
				s.base["c"] = value
			}
		}
	}
	embedding := stringValue(entry["mode"]) == "embedding"
	expression, err := scheduleExpression(s, embedding)
	if err != nil {
		return "", warnings, err
	}
	for _, service := range []string{"flex", "priority"} {
		servicePatches, exists := patches[service]
		if !exists {
			continue
		}
		base := servicePatches[0]
		for dimension := range s.base {
			if _, exists := base[dimension]; !exists {
				return "", warnings, fmt.Errorf("incomplete %s price for %s", service, dimension)
			}
		}
		// Token normalization detects variables across the entire expression.
		// A category present only in one service would disappear from the other
		// branch's p/c amount, so every branch needs its explicit price.
		for dimension := range base {
			if _, exists := s.base[dimension]; !exists {
				return "", warnings, fmt.Errorf("missing standard price for %s service dimension %s", service, dimension)
			}
		}
		serviceSchedule := schedule{base: base}
		contextPrices := s.base
		for _, tier := range s.tiers {
			contextPrices = overlay(contextPrices, tier.prices)
			values, err := subServiceContextPrices(s.base, contextPrices, base, servicePatches[tier.threshold])
			if err != nil {
				return "", warnings, err
			}
			serviceSchedule.tiers = append(serviceSchedule.tiers, priceTier{threshold: tier.threshold, inclusive: tier.inclusive, prices: values})
			delete(servicePatches, tier.threshold)
		}
		for boundary, values := range servicePatches {
			if boundary > 0 {
				serviceSchedule.tiers = append(serviceSchedule.tiers, priceTier{threshold: boundary, prices: values})
			}
		}
		branch, err := scheduleExpression(serviceSchedule, embedding)
		if err != nil {
			return "", warnings, err
		}
		expression = "param(\"service_tier\") == \"" + service + "\" ? (" + branch + ") : (" + expression + ")"
	}
	return expression, warnings, nil
}

func subContextSchedule(entry object, s *schedule) error {
	for key := range entry {
		if !strings.HasPrefix(key, "long_context_") {
			continue
		}
		switch key {
		case "long_context_input_token_threshold", "long_context_threshold_inclusive", "long_context_input_cost_multiplier", "long_context_output_cost_multiplier", "long_context_cache_read_cost_multiplier":
		default:
			return fmt.Errorf("unsupported context price policy: %s", key)
		}
	}
	if raw, exists := entry["tiered_pricing"]; exists {
		if len(s.tiers) > 0 {
			return fmt.Errorf("conflicting context tier formats")
		}
		for key := range entry {
			if strings.HasPrefix(key, "long_context_") && key != "long_context_threshold_inclusive" {
				return fmt.Errorf("conflicting context tier formats")
			}
		}
		return subRangeTiers(raw, entry, s)
	}
	raw, explicit := entry["long_context_input_token_threshold"]
	if !explicit {
		for key := range entry {
			if strings.HasPrefix(key, "long_context_") {
				return fmt.Errorf("long context policy has no threshold")
			}
		}
		// Sub2API scales cache-write/read prices with its input context multiplier
		// when the source lacks an explicit cache price for that tier.
		sort.Slice(s.tiers, func(i, j int) bool { return s.tiers[i].threshold < s.tiers[j].threshold })
		contextPrices := s.base
		for i, tier := range s.tiers {
			contextPrices = overlay(contextPrices, tier.prices)
			values, err := subServiceContextPrices(s.base, contextPrices, s.base, tier.prices)
			if err != nil {
				return err
			}
			s.tiers[i].prices = values
		}
		return nil
	}
	if len(s.tiers) > 0 {
		return fmt.Errorf("conflicting context tier formats")
	}
	boundaryValue, err := price(raw, 0)
	if err != nil {
		return err
	}
	if boundaryValue.IsZero() {
		return nil
	}
	boundary, err := threshold(raw)
	if err != nil {
		return err
	}
	var inclusive bool
	if raw, exists := entry["long_context_threshold_inclusive"]; exists {
		if err := common.Unmarshal(raw, &inclusive); err != nil {
			return fmt.Errorf("invalid context boundary")
		}
	}
	values := make(rates)
	for key, dimension := range map[string]string{"long_context_input_cost_multiplier": "p", "long_context_output_cost_multiplier": "c", "long_context_cache_read_cost_multiplier": "cr"} {
		raw, exists := entry[key]
		if !exists {
			continue
		}
		multiplier, err := price(raw, 0)
		if err != nil {
			return err
		}
		if multiplier.IsZero() {
			continue
		}
		base, exists := s.base[dimension]
		if !exists {
			return fmt.Errorf("context multiplier has no %s price", dimension)
		}
		values[dimension] = base.Mul(multiplier)
	}
	values, err = subServiceContextPrices(s.base, overlay(s.base, values), s.base, values)
	if err != nil {
		return err
	}
	s.tiers = []priceTier{{threshold: boundary, inclusive: inclusive, prices: values}}
	return nil
}

func subRangeTiers(raw common.RawMessage, entry object, s *schedule) error {
	var tiers []object
	if err := common.Unmarshal(raw, &tiers); err != nil || len(tiers) == 0 || len(tiers) > 16 {
		return fmt.Errorf("invalid tiered_pricing")
	}
	var inclusive bool
	if raw, exists := entry["long_context_threshold_inclusive"]; exists {
		if err := common.Unmarshal(raw, &inclusive); err != nil {
			return fmt.Errorf("invalid context boundary")
		}
	}
	previous := int64(0)
	for i, tier := range tiers {
		var bounds []common.RawMessage
		if err := common.Unmarshal(tier["range"], &bounds); err != nil || len(bounds) != 2 {
			return fmt.Errorf("invalid context range")
		}
		lower, err := price(bounds[0], 0)
		if err != nil || !lower.Equal(decimal.NewFromInt(previous)) {
			return fmt.Errorf("non-contiguous context ranges")
		}
		upper, err := threshold(bounds[1])
		if err != nil || upper <= previous {
			return fmt.Errorf("invalid context range end")
		}
		values := make(rates)
		for _, key := range keys(tier) {
			if key == "range" {
				continue
			}
			dimension, exists := subDimensions[key]
			if !exists || dimension == "images" {
				return fmt.Errorf("unsupported context tier field: %s", key)
			}
			if err := addRate(values, dimension, tier[key], 6); err != nil {
				return err
			}
		}
		if i == 0 {
			s.base = overlay(s.base, values)
		} else {
			s.tiers = append(s.tiers, priceTier{threshold: previous, inclusive: inclusive, prices: values})
		}
		previous = upper
	}
	return nil
}

func subServiceContextPrices(base, contextPrices, service, explicit rates) (rates, error) {
	result := make(rates)
	for dimension, value := range service {
		if exact, exists := explicit[dimension]; exists {
			result[dimension] = exact
			continue
		}
		ratioDimension := dimension
		if dimension == "cr" || dimension == "cc" || dimension == "cc1h" {
			ratioDimension = "p"
		}
		before, hasBase := base[ratioDimension]
		after, hasContext := contextPrices[ratioDimension]
		if !hasBase || !hasContext || before.Equal(after) {
			result[dimension] = value
			continue
		}
		if before.IsZero() {
			return nil, fmt.Errorf("cannot derive context multiplier from zero price")
		}
		converted, remainder := value.Mul(after).QuoRem(before, 24)
		if !remainder.IsZero() {
			return nil, fmt.Errorf("context price cannot be converted without rounding")
		}
		result[dimension] = converted
	}
	for key, value := range explicit {
		result[key] = value
	}
	return result, nil
}

// Keep diagnostics deterministic when a source repeats an excluded product.
func uniqueWarnings(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
