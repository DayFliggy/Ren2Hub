package jsplugin

// Ren2Hub extension: the exact usage boundary and the utils.decimal and
// utils.json.parseNumbers helpers. Upstream NewAPI has none of this; its
// usage hooks return plain JavaScript numbers.

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/Calcium-Ion/moejs"
	"github.com/Calcium-Ion/moejs/engine"
	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

const maxSafeInteger = int64(1<<53 - 1)

// maxExactNumberDigits is the most significant digits a JavaScript number may
// carry at the usage boundary. Any decimal of at most 15 significant digits
// survives a round trip through float64, so such a number names exactly the
// decimal its shortest form shows; longer forms are usually arithmetic noise
// (0.1+0.2 = 0.30000000000000004) and are rejected instead of rounded.
const maxExactNumberDigits = 15

// ExactUsageValue is the JS billing boundary. Safe integers become int64,
// non-integer numbers become their shortest decimal text when it has at most
// 15 significant digits, and decimal strings pass through. Anything else is
// an error: the boundary never rounds.
func ExactUsageValue(value any) (any, error) {
	return exactUsageValue(value, 0)
}

func exactUsageResult(key, version, hook string, result any) (any, error) {
	exact, err := ExactUsageValue(result)
	if err != nil {
		return nil, fmt.Errorf("plugin %s@%s hook %s returned inexact usage: %w", key, version, hook, err)
	}
	return exact, nil
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
		return exactInteger(int64(v))
	case int64:
		return exactInteger(v)
	case float64:
		return exactNumber(v)
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

func exactInteger(v int64) (any, error) {
	if v < -maxSafeInteger || v > maxSafeInteger {
		return nil, fmt.Errorf("unsafe plugin integer")
	}
	return v, nil
}

// exactNumber applies the numeric policy to one JavaScript number.
func exactNumber(v float64) (any, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil, fmt.Errorf("plugin billing numbers must be finite")
	}
	if math.Trunc(v) == v {
		if math.Abs(v) > float64(maxSafeInteger) {
			return nil, fmt.Errorf("unsafe plugin integer")
		}
		return int64(v), nil
	}
	shortest := strconv.FormatFloat(v, 'e', -1, 64)
	mantissa, _, _ := strings.Cut(strings.TrimPrefix(shortest, "-"), "e")
	if digits := len(strings.Replace(mantissa, ".", "", 1)); digits > maxExactNumberDigits {
		return nil, fmt.Errorf("plugin billing number %s has %d significant digits; return decimal text instead", strconv.FormatFloat(v, 'g', -1, 64), digits)
	}
	// The 'f' form has the same digits without an exponent.
	return strconv.FormatFloat(v, 'f', -1, 64), nil
}

// usageArgument prepares a usage-hook argument. Source JSON numbers decoded
// as json.Number reach the hook as numbers only when the usage boundary would
// return exactly that decimal again; any other source text stays a string, so
// the host never rounds a value before the hook reads it. changed is false
// when the value holds no json.Number, so it is passed on without a copy.
func usageArgument(value any, depth int) (converted any, changed bool) {
	if depth > maxPluginValueDepth {
		return value, false
	}
	switch v := value.(type) {
	case json.Number:
		return jsonNumberArgument(v), true
	case map[string]any:
		var copied map[string]any
		for key, item := range v {
			if next, itemChanged := usageArgument(item, depth+1); itemChanged {
				if copied == nil {
					copied = make(map[string]any, len(v))
					for k, original := range v {
						copied[k] = original
					}
				}
				copied[key] = next
			}
		}
		if copied == nil {
			return value, false
		}
		return copied, true
	case []any:
		var copied []any
		for index, item := range v {
			if next, itemChanged := usageArgument(item, depth+1); itemChanged {
				if copied == nil {
					copied = append([]any(nil), v...)
				}
				copied[index] = next
			}
		}
		if copied == nil {
			return value, false
		}
		return copied, true
	}
	return value, false
}

func jsonNumberArgument(n json.Number) any {
	source, err := decimal.NewFromString(string(n))
	if err != nil {
		return string(n)
	}
	number, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		return string(n)
	}
	exact, err := exactNumber(number)
	if err != nil {
		return string(n)
	}
	boundary, err := decimal.NewFromString(fmt.Sprint(exact))
	if err != nil || !boundary.Equal(source) {
		return string(n)
	}
	return number
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
		exact, err := exactNumber(v)
		if err != nil {
			return decimal.Zero, err
		}
		text = fmt.Sprint(exact)
	default:
		return decimal.Zero, fmt.Errorf("plugin decimal requires decimal text or an exact number")
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

// decimalArg reads one utils.decimal operand: decimal text or a number the
// usage boundary accepts.
func decimalArg(r *moejs.Realm, value moejs.Value) (decimal.Decimal, error) {
	exported, err := r.ToGoStrict(value)
	if err != nil {
		return decimal.Zero, err
	}
	return pluginDecimal(exported)
}

func decimalHelpers(runtime *moejs.Runtime) map[string]any {
	binary := func(name string, operate func(left, right decimal.Decimal) (decimal.Decimal, error)) moejs.Value {
		return runtime.Function(name, 2, func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			left, err := decimalArg(r, moejs.Arg(args, 0))
			if err != nil {
				return moejs.Undefined(), err
			}
			right, err := decimalArg(r, moejs.Arg(args, 1))
			if err != nil {
				return moejs.Undefined(), err
			}
			result, err := operate(left, right)
			if err != nil {
				return moejs.Undefined(), err
			}
			if result.Exponent() < -72 || result.Exponent() > 72 || len(result.Coefficient().String()) > 256 {
				return moejs.Undefined(), fmt.Errorf("plugin decimal exceeds arithmetic bounds")
			}
			return moejs.String(result.String()), nil
		})
	}
	return map[string]any{
		"string": runtime.Function("string", 1, func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			d, err := decimalArg(r, moejs.Arg(args, 0))
			if err != nil {
				return moejs.Undefined(), err
			}
			return moejs.String(d.String()), nil
		}),
		"bounded": runtime.Function("bounded", 4, func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			bounds := make([]decimal.Decimal, 3)
			for index := range bounds {
				d, err := decimalArg(r, moejs.Arg(args, index))
				if err != nil {
					return moejs.Undefined(), err
				}
				bounds[index] = d
			}
			value, low, high := bounds[0], bounds[1], bounds[2]
			integer := engine.ToBoolean(moejs.Arg(args, 3))
			if value.LessThan(low) || value.GreaterThan(high) || (integer && !value.IsInteger()) {
				return moejs.Undefined(), fmt.Errorf("plugin usage is outside declared bounds")
			}
			return moejs.String(value.String()), nil
		}),
		// number converts an exact decimal back to a JavaScript number only
		// when that round trip preserves the decimal text. Callers use this for
		// legacy provider facts whose wire contract expects ordinary numbers;
		// long or inexact decimals remain text and never get rounded.
		"number": runtime.Function("number", 1, func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			d, err := decimalArg(r, moejs.Arg(args, 0))
			if err != nil {
				return moejs.Undefined(), err
			}
			n, err := strconv.ParseFloat(d.String(), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return moejs.String(d.String()), nil
			}
			exact, err := exactNumber(n)
			if err != nil {
				return moejs.String(d.String()), nil
			}
			candidate, err := pluginDecimal(exact)
			if err != nil || !candidate.Equal(d) {
				return moejs.String(d.String()), nil
			}
			return moejs.Number(n), nil
		}),
		"add": binary("add", func(left, right decimal.Decimal) (decimal.Decimal, error) {
			return left.Add(right), nil
		}),
		"multiply": binary("multiply", func(left, right decimal.Decimal) (decimal.Decimal, error) {
			return left.Mul(right), nil
		}),
		"divide": binary("divide", func(left, right decimal.Decimal) (decimal.Decimal, error) {
			if right.IsZero() {
				return decimal.Zero, fmt.Errorf("plugin division by zero")
			}
			return left.DivRound(right, 36), nil
		}),
	}
}

// parseNumbersHelper is utils.json.parseNumbers: JSON.parse that keeps
// numbers the usage boundary cannot carry exactly as decimal text.
func parseNumbersHelper(runtime *moejs.Runtime) moejs.Value {
	return runtime.Function("parseNumbers", 1, func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		source, err := stringValue(r, moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		if len(source) > MaxJSONToolBytes {
			return moejs.Undefined(), fmt.Errorf("JSON exceeds tool size limit")
		}
		var value any
		if err := common.DecodeJsonNumbers(strings.NewReader(source), &value); err != nil {
			return moejs.Undefined(), err
		}
		exact, err := ExactUsageValue(value)
		if err != nil {
			return moejs.Undefined(), err
		}
		return r.FromGo(exact)
	})
}

func isUsageHook(name string) bool {
	return name == "extractUsage" || name == "extractUsageOnSubmit" || name == "extractUsageOnComplete"
}
