package billing

import (
	"fmt"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

func freezeRequestFacts(schema *billingexpr.UsageSchema, values map[string]interface{}) (map[string]interface{}, error) {
	if schema == nil {
		if len(values) > 0 {
			return nil, fmt.Errorf("request facts require a billing schema")
		}
		return nil, nil
	}
	facts := make(map[string]interface{}, len(values))
	for key, value := range values {
		field, exists := schema.Fields[key]
		if !exists || field.Source != billingexpr.UsageRequestFixed {
			return nil, fmt.Errorf("invalid frozen request fact")
		}
		normalized, err := billingexpr.NormalizeUsageValue(field, value)
		if err != nil {
			return nil, err
		}
		facts[key] = normalized
	}
	for key, field := range schema.Fields {
		if field.Source == billingexpr.UsageRequestFixed && field.Required {
			if _, exists := facts[key]; !exists {
				return nil, fmt.Errorf("required request fact %q is missing", key)
			}
		}
	}
	return facts, nil
}

func resolveUsageFacts(quote *Quote, usage Usage) (map[string]interface{}, error) {
	schema := quote.PriceVersion.UsageSchema
	if schema == nil {
		if len(quote.RequestFacts)+len(usage.EstimatedFacts)+len(usage.ActualFacts) > 0 {
			return nil, fmt.Errorf("usage facts require a billing schema")
		}
		return nil, nil
	}
	facts, err := freezeRequestFacts(schema, quote.RequestFacts)
	if err != nil {
		return nil, err
	}
	if !usage.Estimated && !usage.Complete {
		return nil, fmt.Errorf("actual usage is not complete")
	}
	values := usage.ActualFacts
	if usage.Estimated {
		values = usage.EstimatedFacts
	}
	for key, value := range values {
		field, exists := schema.Fields[key]
		if !exists || field.Source == billingexpr.UsageRequestFixed || (!usage.Estimated && field.Source != billingexpr.UsageCompletionFact) {
			return nil, fmt.Errorf("billing usage fact has an invalid source")
		}
		normalized, err := billingexpr.NormalizeUsageValue(field, value)
		if err != nil {
			return nil, err
		}
		facts[key] = normalized
	}
	for key, field := range schema.Fields {
		if field.Required && (usage.Estimated || field.Source != billingexpr.UsageEstimateOnly) {
			if _, exists := facts[key]; !exists {
				return nil, fmt.Errorf("required billing usage %q is missing", key)
			}
		}
	}
	for _, price := range quote.PriceVersion.Prices {
		if _, exists := facts[price.Quantity]; !exists {
			return nil, fmt.Errorf("priced billing usage %q is missing", price.Quantity)
		}
	}
	return facts, nil
}
