package jsplugin

import (
	"encoding/json"
	"fmt"
	"github.com/grafana/sobek"
	"github.com/shopspring/decimal"
	"math"
	"strconv"
)

const maxSafeInteger = int64(1<<53 - 1)

// ExactUsageValue is the JS billing boundary: fractional values must retain
// their source decimal text, while JS numbers are accepted only as safe integers.
func ExactUsageValue(value any) (any, error) {
	return exactUsageValue(value, 0)
}

// Protocol context can contain fractional settings unrelated to metering. Keep
// these properties lazy: an unused setting must not reject the request, while
// a usage hook that reads an imprecise number must fail before doing arithmetic.
func usageArgumentValue(runtime *sobek.Runtime, value any, depth int) (sobek.Value, error) {
	if depth > 64 {
		return nil, fmt.Errorf("plugin billing JSON exceeds nesting limit")
	}
	exact, err := ExactUsageValue(value)
	if err == nil {
		return runtime.ToValue(exact), nil
	}
	var object *sobek.Object
	var properties map[string]any
	switch typed := value.(type) {
	case map[string]any:
		object, properties = runtime.NewObject(), typed
	case []any:
		object = runtime.NewArray()
		properties = make(map[string]any, len(typed))
		for index, item := range typed {
			properties[strconv.Itoa(index)] = item
		}
		if err := object.Set("length", len(typed)); err != nil {
			return nil, err
		}
	case []map[string]any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = item
		}
		return usageArgumentValue(runtime, items, depth)
	default:
		return nil, err
	}
	for name, item := range properties {
		getter := runtime.ToValue(func(sobek.FunctionCall) sobek.Value {
			converted, err := usageArgumentValue(runtime, item, depth+1)
			if err != nil {
				panic(runtime.NewTypeError("plugin billing input requires preserved decimal text or a safe integer"))
			}
			if err := object.DefineDataProperty(name, converted, sobek.FLAG_TRUE, sobek.FLAG_TRUE, sobek.FLAG_TRUE); err != nil {
				panic(runtime.NewTypeError(err.Error()))
			}
			return converted
		})
		setter := runtime.ToValue(func(call sobek.FunctionCall) sobek.Value {
			if err := object.DefineDataProperty(name, call.Argument(0), sobek.FLAG_TRUE, sobek.FLAG_TRUE, sobek.FLAG_TRUE); err != nil {
				panic(runtime.NewTypeError(err.Error()))
			}
			return sobek.Undefined()
		})
		if err := object.DefineAccessorProperty(name, getter, setter, sobek.FLAG_TRUE, sobek.FLAG_TRUE); err != nil {
			return nil, err
		}
	}
	return object, nil
}

func exactUsageValue(value any, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("plugin billing JSON exceeds nesting limit")
	}
	switch v := value.(type) {
	case nil, string, bool:
		return v, nil
	case json.Number:
		d, err := pluginDecimal(v)
		if err != nil {
			return nil, err
		}
		if d.IsInteger() && d.Abs().LessThanOrEqual(decimal.NewFromInt(maxSafeInteger)) {
			return d.IntPart(), nil
		}
		return string(v), nil
	case int:
		if int64(v) < -maxSafeInteger || int64(v) > maxSafeInteger {
			return nil, fmt.Errorf("unsafe plugin integer")
		}
		return int64(v), nil
	case int64:
		if v < -maxSafeInteger || v > maxSafeInteger {
			return nil, fmt.Errorf("unsafe plugin integer")
		}
		return v, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v || v < -float64(maxSafeInteger) || v > float64(maxSafeInteger) {
			return nil, fmt.Errorf("plugin billing requires decimal text or a safe integer")
		}
		return int64(v), nil
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			exact, err := exactUsageValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[key] = exact
		}
		return result, nil
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			exact, err := exactUsageValue(item, depth+1)
			if err != nil {
				return nil, err
			}
			result[i] = exact
		}
		return result, nil
	case map[string]string:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = item
		}
		return result, nil
	case map[string][]string:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = item
		}
		return exactUsageValue(result, depth+1)
	case []map[string]any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
		}
		return exactUsageValue(result, depth+1)
	case []string:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = item
		}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported plugin billing scalar")
	}
}
func pluginDecimal(value any) (decimal.Decimal, error) {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case json.Number:
		text = string(v)
	case int:
		text = strconv.Itoa(v)
	case int64:
		text = strconv.FormatInt(v, 10)
	case float64:
		exact, err := ExactUsageValue(v)
		if err != nil {
			return decimal.Zero, err
		}
		text = strconv.FormatInt(exact.(int64), 10)
	default:
		return decimal.Zero, fmt.Errorf("plugin decimal requires decimal text or a safe integer")
	}
	if len(text) == 0 || len(text) > 128 {
		return decimal.Zero, fmt.Errorf("invalid plugin decimal")
	}
	d, err := decimal.NewFromString(text)
	if err != nil || d.Exponent() < -36 || d.Exponent() > 36 || len(d.Coefficient().String()) > 128 {
		return decimal.Zero, fmt.Errorf("invalid plugin decimal")
	}
	return d, nil
}
func decimalHelpers() map[string]any {
	return map[string]any{
		"string": func(value any) (string, error) {
			d, err := pluginDecimal(value)
			if err != nil {
				return "", err
			}
			return d.String(), nil
		},
		"bounded": func(value, minimum, maximum any, integer bool) (string, error) {
			d, err := pluginDecimal(value)
			if err != nil {
				return "", err
			}
			low, err := pluginDecimal(minimum)
			if err != nil {
				return "", err
			}
			high, err := pluginDecimal(maximum)
			if err != nil {
				return "", err
			}
			if d.LessThan(low) || d.GreaterThan(high) || (integer && !d.IsInteger()) {
				return "", fmt.Errorf("plugin usage is outside declared bounds")
			}
			return d.String(), nil
		},
		"add":      func(a, b any) (string, error) { return decimalOperation(a, b, "+") },
		"multiply": func(a, b any) (string, error) { return decimalOperation(a, b, "*") },
		"divide":   func(a, b any) (string, error) { return decimalOperation(a, b, "/") },
	}
}
func decimalOperation(a, b any, operator string) (string, error) {
	left, err := pluginDecimal(a)
	if err != nil {
		return "", err
	}
	right, err := pluginDecimal(b)
	if err != nil {
		return "", err
	}
	var result decimal.Decimal
	switch operator {
	case "+":
		result = left.Add(right)
	case "*":
		result = left.Mul(right)
	case "/":
		if right.IsZero() {
			return "", fmt.Errorf("plugin division by zero")
		}
		result = left.DivRound(right, 36)
	}
	if result.Exponent() < -72 || result.Exponent() > 72 || len(result.Coefficient().String()) > 256 {
		return "", fmt.Errorf("plugin decimal exceeds arithmetic bounds")
	}
	return result.String(), nil
}
func isUsageHook(name string) bool {
	return name == "extractUsage" || name == "extractUsageOnSubmit" || name == "extractUsageOnComplete"
}
