package billingexpr

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/relaykit/dto"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/shopspring/decimal"
)

// RunExpr compiles (with cache) and executes an expression string.
// The environment exposes:
//   - p, c             — prompt / completion tokens (auto-excluding separately-priced sub-categories)
//   - len              — total input context length for tier conditions (never reduced by sub-category exclusion)
//   - cr, cc, cc1h     — cache read / creation / creation-1h tokens
//   - tier(name, value) — trace callback that records which tier matched
//   - max, min, abs, ceil, floor — standard math helpers
//
// Returns the resulting float64 quota (before group ratio) and a TraceResult
// with side-channel info captured by tier() during execution.
func RunExpr(exprStr string, params TokenParams) (float64, TraceResult, error) {
	return RunExprWithRequest(exprStr, params, RequestInput{})
}

func RunExprWithRequest(exprStr string, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	entry, err := compileEntryWithSchema(exprStr, ExprHashString(exprStr), request.UsageSchema)
	if err != nil {
		return 0, TraceResult{}, err
	}
	return runEntry(entry, params, request)
}

// RunExprByHash is like RunExpr but accepts a pre-computed hash for the cache
// lookup, avoiding a redundant SHA-256 computation when the caller already
// holds the frozen expression hash.
func RunExprByHash(exprStr, hash string, params TokenParams) (float64, TraceResult, error) {
	return RunExprByHashWithRequest(exprStr, hash, params, RequestInput{})
}

func RunExprByHashWithRequest(exprStr, hash string, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	entry, err := compileEntryWithSchema(exprStr, hash, request.UsageSchema)
	if err != nil {
		return 0, TraceResult{}, err
	}
	return runEntry(entry, params, request)
}

func runEntry(entry *cachedEntry, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	if entry.usedVars["image_count"] {
		if request.ImageCount == nil || *request.ImageCount < 1 || *request.ImageCount > dto.MaxImageN {
			return 0, TraceResult{}, fmt.Errorf("image_count requires an integer between 1 and %d", dto.MaxImageN)
		}
	} else {
		request.ImageCount = nil
	}
	if entry.version == 2 {
		for key := range entry.usedUsage {
			value, exists := request.Usage[key]
			if !exists || value == nil {
				return 0, TraceResult{}, fmt.Errorf("billing usage %q is missing", key)
			}
			if _, err := NormalizeUsageValue(request.UsageSchema.Fields[key], value); err != nil {
				return 0, TraceResult{}, err
			}
		}
	}
	return runProgram(entry.prog, entry.requestRules, params, request)
}

func runProgram(prog *vm.Program, requestRules []RequestRuleTrace, params TokenParams, request RequestInput) (float64, TraceResult, error) {
	trace := TraceResult{
		RequestRules: append([]RequestRuleTrace(nil), requestRules...),
	}
	imageCount := 0
	if request.ImageCount != nil {
		imageCount = *request.ImageCount
		trace.ImageCount = &imageCount
	}
	if err := ValidateAttributes(request.Attributes); err != nil {
		return 0, trace, err
	}
	var probeErr error
	inZone := func(tz string) time.Time {
		if request.FrozenAt.IsZero() {
			probeErr = fmt.Errorf("billing time is not frozen")
			return time.Time{}
		}
		loc, err := time.LoadLocation(strings.TrimSpace(tz))
		if err != nil || strings.TrimSpace(tz) == "" {
			probeErr = fmt.Errorf("invalid billing timezone")
			return time.Time{}
		}
		return request.FrozenAt.In(loc)
	}

	env := map[string]interface{}{
		"image_count": float64(imageCount),
		"p":           params.P,
		"c":           params.C,
		"len":         params.Len,
		"cr":          params.CR,
		"cc":          params.CC,
		"cc1h":        params.CC1h,
		"img":         params.Img,
		"img_cr":      params.ImgCR,
		"img_o":       params.ImgO,
		"ai":          params.AI,
		"ao":          params.AO,
		"reasoning":   params.Reasoning,
		"fixed":       func(usd float64) float64 { return usd * 1000000 },
		"u": func(key string) interface{} {
			if request.UsageSchema == nil {
				probeErr = fmt.Errorf("billing usage schema is missing")
				return nil
			}
			field, exists := request.UsageSchema.Fields[key]
			if !exists {
				probeErr = fmt.Errorf("undeclared billing usage field")
				return nil
			}
			value, err := NormalizeUsageValue(field, request.Usage[key])
			if err != nil {
				probeErr = err
				return nil
			}
			if field.Type == "number" {
				number, err := decimal.NewFromString(value.(string))
				if err != nil {
					probeErr = err
					return nil
				}
				return number.InexactFloat64()
			}
			return value
		},
		"tier": func(name string, value float64) float64 {
			trace.MatchedTier = name
			return value
		},
		requestRuleTraceFunction: func(ruleIndex int, matched bool, multiplier float64) float64 {
			if matched && ruleIndex >= 0 && ruleIndex < len(trace.RequestRules) {
				trace.RequestRules[ruleIndex].Matched = true
			}
			if matched {
				return multiplier
			}
			return 1
		},
		requestRuleTraceIntFunction: func(ruleIndex int, matched bool, multiplier int) int {
			if matched && ruleIndex >= 0 && ruleIndex < len(trace.RequestRules) {
				trace.RequestRules[ruleIndex].Matched = true
			}
			if matched {
				return multiplier
			}
			return 1
		},
		"attr": func(key string) interface{} {
			if !AllowedAttribute(key) {
				probeErr = fmt.Errorf("unregistered billing attribute")
				return nil
			}
			return request.Attributes[key]
		},
		"has": func(source interface{}, substr string) bool {
			if source == nil || substr == "" {
				return false
			}
			return strings.Contains(fmt.Sprint(source), substr)
		},
		"hour":    func(tz string) int { return inZone(tz).Hour() },
		"minute":  func(tz string) int { return inZone(tz).Minute() },
		"weekday": func(tz string) int { return int(inZone(tz).Weekday()) },
		"month":   func(tz string) int { return int(inZone(tz).Month()) },
		"day":     func(tz string) int { return inZone(tz).Day() },
		"max":     math.Max,
		"min":     math.Min,
		"abs":     math.Abs,
		"ceil":    math.Ceil,
		"floor":   math.Floor,
	}

	out, err := expr.Run(prog, env)
	if probeErr != nil {
		return 0, trace, probeErr
	}
	if err != nil {
		return 0, trace, fmt.Errorf("expr run error: %w", err)
	}
	f, ok := out.(float64)
	if !ok {
		return 0, trace, fmt.Errorf("expr result is %T, want float64", out)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return 0, trace, fmt.Errorf("invalid billing expression result")
	}
	return f, trace, nil
}

func AllowedAttribute(name string) bool {
	switch name {
	case "service_tier", "quality", "resolution", "quantity", "duration", "tool_category", "effort", "thinking", "thinking_budget_tokens", "temperature", "top_p":
		return true
	}
	return false
}

func ValidateAttributes(attributes map[string]interface{}) error {
	for name, value := range attributes {
		if !AllowedAttribute(name) {
			return fmt.Errorf("unregistered billing attribute")
		}
		switch v := value.(type) {
		case string:
			if len(v) > 128 {
				return fmt.Errorf("billing attribute too long")
			}
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > math.MaxInt32 {
				return fmt.Errorf("invalid billing attribute")
			}
		case int:
			if v < 0 || int64(v) > math.MaxInt32 {
				return fmt.Errorf("invalid billing attribute")
			}
		case bool, nil:
		default:
			return fmt.Errorf("invalid billing attribute type")
		}
	}
	return nil
}
