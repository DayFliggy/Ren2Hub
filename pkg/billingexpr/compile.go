package billingexpr

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

const maxCacheSize = 256

// DefaultExprVersion is used when an expression string has no version prefix.
const DefaultExprVersion = 1

const (
	requestRuleTraceFunction    = "_trace"
	requestRuleTraceIntFunction = "_trace_int"
)

// ParseExprVersion extracts the version tag and body from an expression string.
// Format: "v1:tier(...)" → version=1, body="tier(...)".
// No prefix defaults to DefaultExprVersion.
func ParseExprVersion(exprStr string) (version int, body string) {
	if prefix, rest, found := strings.Cut(exprStr, ":"); found && strings.HasPrefix(prefix, "v") {
		if parsed, err := strconv.Atoi(strings.TrimPrefix(prefix, "v")); err == nil {
			return parsed, rest
		}
	}
	return DefaultExprVersion, exprStr
}

// requestRulePatcher adds trace side effects to existing request multipliers
// without changing the stored expression or its numeric result.
type requestRulePatcher struct {
	requestRules         []RequestRuleTrace
	restrictedIdentifier string
	validationError      error
	usageFields          map[string]UsageField
	usedUsage            map[string]bool
	usageIdentifiers     int
	usageCalls           int
	usesImageCount       bool
}

func (p *requestRulePatcher) Visit(node *ast.Node) {
	if identifier, ok := (*node).(*ast.IdentifierNode); ok {
		if identifier.Value == "image_count" {
			p.usesImageCount = true
		}
		if identifier.Value == "u" {
			p.usageIdentifiers++
		}
		switch identifier.Value {
		case requestRuleTraceFunction, requestRuleTraceIntFunction, "param", "header":
			p.restrictedIdentifier = identifier.Value
		}
		return
	}
	if call, ok := (*node).(*ast.CallNode); ok {
		if callee, ok := call.Callee.(*ast.IdentifierNode); ok {
			if callee.Value == "u" {
				p.usageCalls++
			}
			switch callee.Value {
			case "u", "attr", "hour", "minute", "weekday", "month", "day":
				if len(call.Arguments) != 1 {
					p.validationError = fmt.Errorf("billing probe requires one literal argument")
					return
				}
				arg, ok := call.Arguments[0].(*ast.StringNode)
				if !ok {
					p.validationError = fmt.Errorf("billing probe requires a literal argument")
					return
				}
				if callee.Value == "u" {
					if _, exists := p.usageFields[arg.Value]; !exists {
						p.validationError = fmt.Errorf("undeclared billing usage field %q", arg.Value)
					} else {
						p.usedUsage[arg.Value] = true
					}
				} else if callee.Value == "attr" {
					if !AllowedAttribute(arg.Value) {
						p.validationError = fmt.Errorf("unregistered billing attribute")
					}
				} else {
					if _, err := time.LoadLocation(arg.Value); err != nil || arg.Value == "" || arg.Value == "Local" {
						p.validationError = fmt.Errorf("invalid billing timezone")
					}
				}
			}
		}
	}

	conditional, ok := (*node).(*ast.ConditionalNode)
	if !ok || !conditional.Ternary || !usesRequestProbe(conditional.Cond) {
		return
	}
	multiplier, ok := requestRuleNumber(conditional.Exp1)
	fallback, fallbackOK := requestRuleNumber(conditional.Exp2)
	if !ok || !fallbackOK || fallback != 1 {
		return
	}

	ruleIndex := len(p.requestRules)
	p.requestRules = append(p.requestRules, RequestRuleTrace{
		Cond:       conditional.Cond.String(),
		Multiplier: strconv.FormatFloat(multiplier, 'f', -1, 64),
	})

	traceFunction := requestRuleTraceFunction
	var multiplierNode ast.Node = &ast.FloatNode{Value: multiplier}
	if _, multiplierIsInt := conditional.Exp1.(*ast.IntegerNode); multiplierIsInt {
		if _, fallbackIsInt := conditional.Exp2.(*ast.IntegerNode); fallbackIsInt {
			traceFunction = requestRuleTraceIntFunction
			multiplierNode = conditional.Exp1
		}
	}

	ast.Patch(node, &ast.CallNode{
		Callee: &ast.IdentifierNode{Value: traceFunction},
		Arguments: []ast.Node{
			&ast.IntegerNode{Value: ruleIndex},
			conditional.Cond,
			multiplierNode,
		},
	})
}

func requestRuleNumber(node ast.Node) (float64, bool) {
	switch value := node.(type) {
	case *ast.IntegerNode:
		return float64(value.Value), true
	case *ast.FloatNode:
		return value.Value, true
	default:
		return 0, false
	}
}

func usesRequestProbe(node ast.Node) bool {
	return ast.Find(node, func(node ast.Node) bool {
		identifier, ok := node.(*ast.IdentifierNode)
		if !ok {
			return false
		}
		switch identifier.Value {
		case "attr", "hour", "minute", "weekday", "month", "day":
			return true
		default:
			return false
		}
	}) != nil
}

type cachedEntry struct {
	prog         *vm.Program
	usedVars     map[string]bool
	requestRules []RequestRuleTrace
	version      int
	usedUsage    map[string]bool
}

var (
	cacheMu sync.RWMutex
	cache   = make(map[string]*cachedEntry, 64)
)

// compileEnvPrototypeV1 is the v1 type-checking prototype used at compile time.
var compileEnvPrototypeV1 = map[string]interface{}{
	"image_count": float64(0),
	"p":           float64(0),
	"c":           float64(0),
	"len":         float64(0),
	"cr":          float64(0),
	"cc":          float64(0),
	"cc1h":        float64(0),
	"img":         float64(0),
	"img_cr":      float64(0),
	"img_o":       float64(0),
	"ai":          float64(0),
	"ao":          float64(0),
	"reasoning":   float64(0),
	"tier":        func(string, float64) float64 { return 0 },
	"fixed":       func(float64) float64 { return 0 },
	"_trace":      func(int, bool, float64) float64 { return 1 },
	"_trace_int":  func(int, bool, int) int { return 1 },
	"attr":        func(string) interface{} { return nil },
	"has":         func(interface{}, string) bool { return false },
	"hour":        func(string) int { return 0 },
	"minute":      func(string) int { return 0 },
	"weekday":     func(string) int { return 0 },
	"month":       func(string) int { return 0 },
	"day":         func(string) int { return 0 },
	"max":         math.Max,
	"min":         math.Min,
	"abs":         math.Abs,
	"ceil":        math.Ceil,
	"floor":       math.Floor,
}

func getCompileEnv(version int) map[string]interface{} {
	if version != 2 {
		return compileEnvPrototypeV1
	}
	env := make(map[string]interface{}, len(compileEnvPrototypeV1))
	for key, value := range compileEnvPrototypeV1 {
		switch key {
		case "p", "c", "reasoning", "len", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao", "fixed", "image_count":
			continue
		}
		env[key] = value
	}
	env["u"] = func(string) interface{} { return nil }
	return env
}

// CompileFromCache compiles an expression string, using a cached program when
// available. The cache is keyed by the SHA-256 hex digest of the expression.
func CompileFromCache(exprStr string) (*vm.Program, error) {
	return compileFromCacheByHash(exprStr, ExprHashString(exprStr))
}

// CompileFromCacheByHash is like CompileFromCache but accepts a pre-computed
// hash, useful when the caller already has the frozen expression hash.
func CompileFromCacheByHash(exprStr, hash string) (*vm.Program, error) {
	return compileFromCacheByHash(exprStr, hash)
}

func compileFromCacheByHash(exprStr, hash string) (*vm.Program, error) {
	entry, err := compileEntryFromCacheByHash(exprStr, hash)
	if err != nil {
		return nil, err
	}
	return entry.prog, nil
}

func compileEntryFromCacheByHash(exprStr, hash string) (*cachedEntry, error) {
	return compileEntryWithSchema(exprStr, hash, nil)
}

func compileEntryWithSchema(exprStr, hash string, schema *UsageSchema) (*cachedEntry, error) {
	if hash != ExprHashString(exprStr) {
		return nil, fmt.Errorf("billing expression hash mismatch")
	}
	version, body := ParseExprVersion(exprStr)
	if version != 1 && version != 2 {
		return nil, fmt.Errorf("unsupported billing expression version")
	}
	if version == 2 {
		if err := ValidateUsageSchema(schema); err != nil {
			return nil, err
		}
		hash += ":" + schema.Hash
	} else if schema != nil {
		return nil, fmt.Errorf("token expressions cannot use a task usage schema")
	}
	cacheMu.RLock()
	if entry, ok := cache[hash]; ok {
		cacheMu.RUnlock()
		return entry, nil
	}
	cacheMu.RUnlock()

	patcher := &requestRulePatcher{usedUsage: map[string]bool{}}
	if schema != nil {
		patcher.usageFields = schema.Fields
	}
	prog, err := expr.Compile(body, expr.Env(getCompileEnv(version)), expr.Patch(patcher), expr.AsFloat64())
	if patcher.usageIdentifiers != patcher.usageCalls {
		return nil, fmt.Errorf("usage accessor must be called directly with a literal key")
	}
	if patcher.validationError != nil {
		return nil, patcher.validationError
	}
	if patcher.restrictedIdentifier != "" {
		return nil, fmt.Errorf("expr compile error: identifier %q is reserved for internal use", patcher.restrictedIdentifier)
	}
	if err != nil {
		return nil, fmt.Errorf("expr compile error: %w", err)
	}

	entry := &cachedEntry{
		prog:         prog,
		usedVars:     extractUsedVars(prog),
		requestRules: patcher.requestRules,
		version:      version,
		usedUsage:    patcher.usedUsage,
	}
	// An unreachable branch still owns the image dimension. Optimizer folding
	// must not bypass quantity validation or permit a second per-image price.
	if patcher.usesImageCount {
		entry.usedVars["image_count"] = true
	}
	cacheMu.Lock()
	if len(cache) >= maxCacheSize {
		cache = make(map[string]*cachedEntry, 64)
	}
	cache[hash] = entry
	cacheMu.Unlock()

	return entry, nil
}

// CompileWithUsageSchema validates every usage reference, including inactive
// branches. A compiled result cannot be reused under a different contract.
func CompileWithUsageSchema(expression string, schema *UsageSchema) (*vm.Program, error) {
	entry, err := compileEntryWithSchema(expression, ExprHashString(expression), schema)
	if err != nil {
		return nil, err
	}
	return entry.prog, nil
}

func UsedUsageFields(expression string, schema *UsageSchema) (map[string]bool, error) {
	entry, err := compileEntryWithSchema(expression, ExprHashString(expression), schema)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]bool, len(entry.usedUsage))
	for key, value := range entry.usedUsage {
		fields[key] = value
	}
	return fields, nil
}

// ExprVersion returns the version of a cached expression. Returns DefaultExprVersion
// if the expression hasn't been compiled yet or is empty.
func ExprVersion(exprStr string) int {
	if exprStr == "" {
		return DefaultExprVersion
	}
	hash := ExprHashString(exprStr)
	cacheMu.RLock()
	if entry, ok := cache[hash]; ok {
		cacheMu.RUnlock()
		return entry.version
	}
	cacheMu.RUnlock()
	v, _ := ParseExprVersion(exprStr)
	return v
}

func extractUsedVars(prog *vm.Program) map[string]bool {
	vars := make(map[string]bool)
	node := prog.Node()
	ast.Find(node, func(n ast.Node) bool {
		if id, ok := n.(*ast.IdentifierNode); ok {
			switch id.Value {
			case requestRuleTraceFunction, requestRuleTraceIntFunction:
				return false
			}
			vars[id.Value] = true
		}
		return false
	})
	return vars
}

// UsedVars returns the set of identifier names referenced by an expression.
// The result is cached alongside the compiled program. Returns nil for empty input.
func UsedVars(exprStr string) map[string]bool {
	if exprStr == "" {
		return nil
	}
	hash := ExprHashString(exprStr)
	cacheMu.RLock()
	if entry, ok := cache[hash]; ok {
		cacheMu.RUnlock()
		return entry.usedVars
	}
	cacheMu.RUnlock()

	// Compile (and cache) to populate usedVars
	if _, err := compileFromCacheByHash(exprStr, hash); err != nil {
		return nil
	}
	cacheMu.RLock()
	entry, ok := cache[hash]
	cacheMu.RUnlock()
	if ok {
		return entry.usedVars
	}
	return nil
}

// InvalidateCache clears the compiled-expression cache.
// Called when billing rules are updated.
func InvalidateCache() {
	cacheMu.Lock()
	cache = make(map[string]*cachedEntry, 64)
	cacheMu.Unlock()
}
