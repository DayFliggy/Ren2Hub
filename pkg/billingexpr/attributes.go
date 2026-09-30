package billingexpr

import (
	"encoding/json"
	"fmt"
	"github.com/shopspring/decimal"
)

func AllowedAttribute(name string) bool {
	switch name {
	case "service_tier", "quality", "resolution", "quantity", "duration", "tool_category", "effort", "thinking", "thinking_budget_tokens", "temperature", "top_p":
		return true
	}
	return false
}
func numericAttribute(name string) bool {
	switch name {
	case "quantity", "duration", "thinking_budget_tokens", "temperature", "top_p":
		return true
	}
	return false
}

// AttributeNumber is the only numeric attribute boundary. Floats are rejected:
// transport readers must preserve JSON number text before a quote is frozen.
func AttributeNumber(value any) (decimal.Decimal, error) {
	var raw string
	switch v := value.(type) {
	case string:
		raw = v
	case json.Number:
		raw = string(v)
	case decimal.Decimal:
		raw = v.String()
	case int:
		raw = fmt.Sprint(v)
	case int64:
		raw = fmt.Sprint(v)
	default:
		return decimal.Zero, fmt.Errorf("billing attribute requires an exact number")
	}
	return boundedUsageDecimal(raw)
}
func ValidateAttributes(attributes map[string]interface{}) error {
	for name, value := range attributes {
		if !AllowedAttribute(name) {
			return fmt.Errorf("unregistered billing attribute")
		}
		if value == nil {
			continue
		}
		if numericAttribute(name) {
			if _, err := AttributeNumber(value); err != nil {
				return err
			}
			continue
		}
		switch v := value.(type) {
		case string:
			if len(v) > 128 {
				return fmt.Errorf("billing attribute too long")
			}
		case bool:
		default:
			return fmt.Errorf("invalid billing attribute type")
		}
	}
	return nil
}
func FreezeAttributes(attributes map[string]interface{}) (map[string]interface{}, error) {
	if err := ValidateAttributes(attributes); err != nil {
		return nil, err
	}
	result := make(map[string]interface{}, len(attributes))
	for key, value := range attributes {
		if numericAttribute(key) && value != nil {
			number, err := AttributeNumber(value)
			if err != nil {
				return nil, err
			}
			result[key] = number.String()
		} else {
			result[key] = value
		}
	}
	return result, nil
}
