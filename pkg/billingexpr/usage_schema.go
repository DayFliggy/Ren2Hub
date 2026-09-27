package billingexpr

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const UsageSchemaVersion = 1

const (
	UsageRequestFixed   = "request_fixed"
	UsageEstimateOnly   = "estimate_only"
	UsageCompletionFact = "completion_fact"
)

type UsageField struct {
	Type     string   `json:"type"`
	Unit     string   `json:"unit,omitempty"`
	Source   string   `json:"source"`
	Required bool     `json:"required"`
	Integer  bool     `json:"integer,omitempty"`
	Minimum  string   `json:"minimum,omitempty"`
	Maximum  string   `json:"maximum,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

type UsageSchema struct {
	Version int                   `json:"version"`
	Fields  map[string]UsageField `json:"fields"`
	Hash    string                `json:"hash"`
}

var usageFieldName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func FreezeUsageSchema(schema *UsageSchema) (*UsageSchema, error) {
	if schema == nil {
		return nil, fmt.Errorf("billing usage schema is missing")
	}
	copySchema := &UsageSchema{Version: schema.Version, Fields: make(map[string]UsageField, len(schema.Fields))}
	for key, field := range schema.Fields {
		field.Enum = slices.Clone(field.Enum)
		copySchema.Fields[key] = field
	}
	encoded, err := common.Marshal(copySchema)
	if err != nil {
		return nil, err
	}
	copySchema.Hash = ExprHashString(string(encoded))
	if schema.Hash != "" && schema.Hash != copySchema.Hash {
		return nil, fmt.Errorf("billing usage schema integrity mismatch")
	}
	if err := ValidateUsageSchema(copySchema); err != nil {
		return nil, err
	}
	return copySchema, nil
}

func ValidateUsageSchema(schema *UsageSchema) error {
	if schema == nil || schema.Version != UsageSchemaVersion || len(schema.Fields) == 0 || len(schema.Fields) > 64 {
		return fmt.Errorf("unsupported billing usage schema")
	}
	for key, field := range schema.Fields {
		if !usageFieldName.MatchString(key) {
			return fmt.Errorf("invalid billing usage field")
		}
		switch field.Source {
		case UsageRequestFixed, UsageEstimateOnly, UsageCompletionFact:
		default:
			return fmt.Errorf("invalid billing usage field source")
		}
		switch field.Type {
		case "number":
			if field.Unit == "" || len(field.Unit) > 64 || len(field.Enum) > 0 {
				return fmt.Errorf("numeric billing usage requires an explicit unit")
			}
			low, high, err := usageBounds(field)
			if err != nil || low.GreaterThan(high) {
				return fmt.Errorf("invalid billing usage bounds")
			}
		case "string", "boolean":
			if field.Integer || field.Minimum != "" || field.Maximum != "" || field.Unit != "" || (field.Type == "boolean" && len(field.Enum) != 0) {
				return fmt.Errorf("invalid categorical billing usage schema")
			}
			if len(field.Enum) > 128 {
				return fmt.Errorf("too many billing usage enum values")
			}
			seen := map[string]bool{}
			for _, option := range field.Enum {
				if option == "" || len(option) > 128 || seen[option] {
					return fmt.Errorf("invalid billing usage enum")
				}
				seen[option] = true
			}
		default:
			return fmt.Errorf("unknown billing usage type")
		}
	}
	copySchema := *schema
	copySchema.Hash = ""
	encoded, err := common.Marshal(copySchema)
	if err != nil || schema.Hash == "" || ExprHashString(string(encoded)) != schema.Hash {
		return fmt.Errorf("billing usage schema integrity mismatch")
	}
	return nil
}

func usageBounds(field UsageField) (decimal.Decimal, decimal.Decimal, error) {
	low, high := decimal.Zero, decimal.NewFromInt(math.MaxInt32)
	var err error
	if field.Minimum != "" {
		low, err = boundedUsageDecimal(field.Minimum)
		if err != nil {
			return low, high, err
		}
	}
	if field.Maximum != "" {
		high, err = boundedUsageDecimal(field.Maximum)
	}
	return low, high, err
}

func boundedUsageDecimal(value string) (decimal.Decimal, error) {
	if len(value) == 0 || len(value) > 64 {
		return decimal.Zero, fmt.Errorf("invalid billing usage number")
	}
	number, err := decimal.NewFromString(value)
	if err != nil || number.IsNegative() || number.Exponent() < -18 || number.Exponent() > 18 || number.GreaterThan(decimal.NewFromInt(math.MaxInt32)) {
		return decimal.Zero, fmt.Errorf("invalid billing usage number")
	}
	return number, nil
}

// NormalizeUsageValue accepts scalar facts only. Decimal quantities can be
// transported as strings without passing through JSON floating-point rounding.
func NormalizeUsageValue(field UsageField, value interface{}) (interface{}, error) {
	switch field.Type {
	case "number":
		var raw string
		switch number := value.(type) {
		case string:
			raw = number
		case json.Number:
			raw = string(number)
		case decimal.Decimal:
			raw = number.String()
		case int:
			raw = fmt.Sprint(number)
		case int64:
			raw = fmt.Sprint(number)
		default:
			return nil, fmt.Errorf("billing usage requires a number")
		}
		number, err := boundedUsageDecimal(raw)
		if err != nil {
			return nil, err
		}
		low, high, err := usageBounds(field)
		if err != nil || number.LessThan(low) || number.GreaterThan(high) || (field.Integer && !number.Equal(number.Truncate(0))) {
			return nil, fmt.Errorf("billing usage is outside schema bounds")
		}
		return number.String(), nil
	case "string":
		text, ok := value.(string)
		if !ok || len(text) > 128 || (len(field.Enum) > 0 && !slices.Contains(field.Enum, text)) {
			return nil, fmt.Errorf("invalid billing usage string")
		}
		return text, nil
	case "boolean":
		flag, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("billing usage requires a boolean")
		}
		return flag, nil
	default:
		return nil, fmt.Errorf("unknown billing usage type")
	}
}
