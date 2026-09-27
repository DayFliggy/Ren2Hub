package billingexpr

import (
	"fmt"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser/lexer"
	"github.com/shopspring/decimal"
	"strconv"
	"strings"
	"sync"
)

const DefaultExprVersion = 1

// Plan is a restricted, unoptimized Decimal execution plan. Only the Expr
// lexer is reused: numeric literal text never passes through its VM or float AST.
type Plan struct {
	root                       *operation
	version                    int
	usedVars, usedUsage, zones map[string]bool
	rules                      []RequestRuleTrace
}
type operation struct {
	kind, text string
	number     decimal.Decimal
	args       []*operation
	rule       int
}

var cacheMu sync.RWMutex
var cache = map[string]*Plan{}

func ParseExprVersion(s string) (int, string) {
	if prefix, body, ok := strings.Cut(s, ":"); ok && strings.HasPrefix(prefix, "v") {
		if v, e := strconv.Atoi(prefix[1:]); e == nil {
			return v, body
		}
	}
	return DefaultExprVersion, s
}
func ExprVersion(s string) int                          { v, _ := ParseExprVersion(s); return v }
func CompileFromCache(s string) (*Plan, error)          { return compilePlan(s, ExprHashString(s), nil) }
func CompileFromCacheByHash(s, h string) (*Plan, error) { return compilePlan(s, h, nil) }
func CompileWithUsageSchema(s string, schema *UsageSchema) (*Plan, error) {
	return compilePlan(s, ExprHashString(s), schema)
}
func compilePlan(s, h string, schema *UsageSchema) (*Plan, error) {
	if h != ExprHashString(s) {
		return nil, fmt.Errorf("billing expression hash mismatch")
	}
	v, body := ParseExprVersion(s)
	if v != 1 && v != 2 {
		return nil, fmt.Errorf("unsupported billing expression version")
	}
	if len(body) == 0 || len(body) > 16384 {
		return nil, fmt.Errorf("invalid billing expression length")
	}
	if v == 2 {
		if e := ValidateUsageSchema(schema); e != nil {
			return nil, e
		}
		h += ":" + schema.Hash
	} else if schema != nil {
		return nil, fmt.Errorf("token expressions cannot use a task usage schema")
	}
	cacheMu.RLock()
	hit := cache[h]
	cacheMu.RUnlock()
	if hit != nil {
		return hit, nil
	}
	tokens, e := lexer.Lex(file.NewSource(body))
	if e != nil {
		return nil, e
	}
	if len(tokens) > 2048 {
		return nil, fmt.Errorf("billing expression too complex")
	}
	plan := &Plan{version: v, usedVars: map[string]bool{}, usedUsage: map[string]bool{}, zones: map[string]bool{}}
	p := planParser{tokens: tokens, plan: plan, schema: schema}
	plan.root, e = p.expression(0, 0)
	if e != nil {
		return nil, e
	}
	if p.peek().Kind != lexer.EOF {
		return nil, fmt.Errorf("unsupported billing syntax %q", p.peek().Value)
	}
	resultType, err := operationType(plan.root, schema)
	if err != nil {
		return nil, err
	}
	if resultType != "number" {
		return nil, fmt.Errorf("billing expression must return a number")
	}
	cacheMu.Lock()
	if len(cache) >= 256 {
		cache = map[string]*Plan{}
	}
	cache[h] = plan
	cacheMu.Unlock()
	return plan, nil
}

type planParser struct {
	tokens []lexer.Token
	pos    int
	plan   *Plan
	schema *UsageSchema
}

func (p *planParser) peek() lexer.Token {
	if p.pos >= len(p.tokens) {
		return lexer.Token{Kind: lexer.EOF}
	}
	return p.tokens[p.pos]
}
func (p *planParser) take(s string) bool {
	if p.peek().Value != s {
		return false
	}
	p.pos++
	return true
}
func precedence(s string) int {
	switch s {
	case "||", "or":
		return 1
	case "&&", "and":
		return 2
	case "==", "!=":
		return 3
	case "<", "<=", ">", ">=":
		return 4
	case "+", "-":
		return 5
	case "*", "/":
		return 6
	}
	return -1
}
func (p *planParser) expression(min, depth int) (*operation, error) {
	if depth > 128 {
		return nil, fmt.Errorf("billing nesting exceeds limit")
	}
	t := p.peek()
	p.pos++
	var n *operation
	var e error
	switch {
	case t.Kind == lexer.Bracket && t.Value == "(":
		n, e = p.expression(0, depth+1)
		if e == nil && !p.take(")") {
			e = fmt.Errorf("missing billing parenthesis")
		}
	case t.Kind == lexer.Operator && (t.Value == "+" || t.Value == "-" || t.Value == "!" || t.Value == "not"):
		var child *operation
		child, e = p.expression(7, depth+1)
		n = &operation{kind: "unary", text: t.Value, args: []*operation{child}, rule: -1}
	case t.Kind == lexer.Number:
		if len(t.Value) > 256 {
			return nil, fmt.Errorf("billing literal exceeds limit")
		}
		var d decimal.Decimal
		d, e = decimal.NewFromString(t.Value)
		if e == nil {
			e = checkDecimal(d)
		}
		n = &operation{kind: "number", text: t.Value, number: d, rule: -1}
	case t.Kind == lexer.String:
		if len(t.Value) > 128 {
			return nil, fmt.Errorf("billing string exceeds limit")
		}
		n = &operation{kind: "string", text: t.Value, rule: -1}
	case t.Kind == lexer.Identifier:
		switch t.Value {
		case "true", "false", "nil":
			n = &operation{kind: t.Value, rule: -1}
		default:
			if p.take("(") {
				n, e = p.call(t.Value, depth+1)
			} else {
				if p.plan.version != 1 || !isTokenVariable(t.Value) {
					return nil, fmt.Errorf("unsupported billing identifier %q", t.Value)
				}
				p.plan.usedVars[t.Value] = true
				n = &operation{kind: "variable", text: t.Value, rule: -1}
			}
		}
	default:
		return nil, fmt.Errorf("unsupported billing syntax %q", t.Value)
	}
	if e != nil {
		return nil, e
	}
	for {
		op := p.peek().Value
		priority := precedence(op)
		if p.peek().Kind != lexer.Operator || priority < min || priority < 0 {
			break
		}
		p.pos++
		right, e := p.expression(priority+1, depth+1)
		if e != nil {
			return nil, e
		}
		n = &operation{kind: "binary", text: op, args: []*operation{n, right}, rule: -1}
	}
	if min == 0 && p.take("?") {
		yes, e := p.expression(0, depth+1)
		if e != nil {
			return nil, e
		}
		if !p.take(":") {
			return nil, fmt.Errorf("missing billing conditional alternative")
		}
		no, e := p.expression(0, depth+1)
		if e != nil {
			return nil, e
		}
		n = &operation{kind: "conditional", args: []*operation{n, yes, no}, rule: -1}
		if usesProbe(n.args[0]) && yes.kind == "number" && no.kind == "number" && no.number.Equal(decimal.NewFromInt(1)) {
			n.rule = len(p.plan.rules)
			p.plan.rules = append(p.plan.rules, RequestRuleTrace{Cond: renderOperation(n.args[0]), Multiplier: yes.number.String()})
		}
	}
	return n, nil
}
func (p *planParser) call(name string, depth int) (*operation, error) {
	counts := map[string]int{"tier": 2, "fixed": 1, "u": 1, "attr": 1, "attr_num": 1, "has": 2, "hour": 1, "minute": 1, "weekday": 1, "month": 1, "day": 1, "max": 2, "min": 2, "abs": 1, "ceil": 1, "floor": 1}
	count, ok := counts[name]
	if !ok || (name == "fixed" && p.plan.version != 1) || (name == "u" && p.plan.version != 2) {
		return nil, fmt.Errorf("unsupported billing function %q", name)
	}
	n := &operation{kind: "call", text: name, rule: -1}
	if !p.take(")") {
		for {
			a, e := p.expression(0, depth+1)
			if e != nil {
				return nil, e
			}
			n.args = append(n.args, a)
			if len(n.args) > count {
				return nil, fmt.Errorf("invalid billing function arguments")
			}
			if p.take(")") {
				break
			}
			if !p.take(",") {
				return nil, fmt.Errorf("invalid billing function arguments")
			}
		}
	}
	if len(n.args) != count {
		return nil, fmt.Errorf("invalid billing function arguments")
	}
	p.plan.usedVars[name] = true
	switch name {
	case "u", "attr", "attr_num", "hour", "minute", "weekday", "month", "day", "tier":
		if n.args[0].kind != "string" {
			return nil, fmt.Errorf("billing probe requires a literal argument")
		}
		key := n.args[0].text
		switch name {
		case "u":
			if _, ok := p.schema.Fields[key]; !ok {
				return nil, fmt.Errorf("undeclared billing usage field %q", key)
			}
			p.plan.usedUsage[key] = true
		case "attr", "attr_num":
			if !AllowedAttribute(key) || (name == "attr_num" && !numericAttribute(key)) {
				return nil, fmt.Errorf("unregistered billing attribute")
			}
		case "tier":
		default:
			if !validZoneName(key) {
				return nil, fmt.Errorf("invalid billing timezone")
			}
			p.plan.zones[key] = true
		}
	}
	return n, nil
}
func isTokenVariable(s string) bool {
	switch s {
	case "p", "c", "len", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ai_cr", "ao", "reasoning", "image_count":
		return true
	}
	return false
}
func usesProbe(n *operation) bool {
	if n.kind == "call" {
		switch n.text {
		case "attr", "attr_num", "hour", "minute", "weekday", "month", "day":
			return true
		}
	}
	for _, a := range n.args {
		if usesProbe(a) {
			return true
		}
	}
	return false
}
func renderOperation(n *operation) string {
	switch n.kind {
	case "string":
		return strconv.Quote(n.text)
	case "number", "variable":
		return n.text
	case "call":
		parts := make([]string, len(n.args))
		for i, a := range n.args {
			parts[i] = renderOperation(a)
		}
		return n.text + "(" + strings.Join(parts, ", ") + ")"
	case "binary":
		return "(" + renderOperation(n.args[0]) + " " + n.text + " " + renderOperation(n.args[1]) + ")"
	case "unary":
		return n.text + renderOperation(n.args[0])
	}
	return n.kind
}
func cloneSet(m map[string]bool) map[string]bool {
	r := make(map[string]bool, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}
func UsedVars(s string) map[string]bool {
	p, e := CompileFromCache(s)
	if e != nil {
		return nil
	}
	return cloneSet(p.usedVars)
}
func UsedUsageFields(s string, schema *UsageSchema) (map[string]bool, error) {
	p, e := CompileWithUsageSchema(s, schema)
	if e != nil {
		return nil, e
	}
	return cloneSet(p.usedUsage), nil
}
func InvalidateCache() { cacheMu.Lock(); cache = map[string]*Plan{}; cacheMu.Unlock() }

// Type-check every branch before execution. Unknown request attributes stay
// nullable scalars; numeric arithmetic requires attr_num explicitly.
func operationType(n *operation, schema *UsageSchema) (string, error) {
	types := make([]string, len(n.args))
	for i, child := range n.args {
		typ, err := operationType(child, schema)
		if err != nil {
			return "", err
		}
		types[i] = typ
	}
	err := fmt.Errorf("invalid types in billing expression")
	switch n.kind {
	case "number", "variable":
		return "number", nil
	case "string":
		return "string", nil
	case "true", "false":
		return "boolean", nil
	case "nil":
		return "nil", nil
	case "unary":
		if n.text == "!" || n.text == "not" {
			if types[0] != "boolean" {
				return "", err
			}
			return "boolean", nil
		}
		if types[0] != "number" {
			return "", err
		}
		return "number", nil
	case "conditional":
		if types[0] != "boolean" || types[1] != types[2] {
			return "", err
		}
		return types[1], nil
	case "binary":
		switch n.text {
		case "==", "!=":
			return "boolean", nil
		case "&&", "and", "||", "or":
			if types[0] != "boolean" || types[1] != "boolean" {
				return "", err
			}
			return "boolean", nil
		}
		if types[0] != "number" || types[1] != "number" {
			return "", err
		}
		switch n.text {
		case "<", "<=", ">", ">=":
			return "boolean", nil
		}
		return "number", nil
	case "call":
		switch n.text {
		case "u":
			return schema.Fields[n.args[0].text].Type, nil
		case "attr":
			return "scalar", nil
		case "attr_num", "hour", "minute", "weekday", "month", "day":
			return "number", nil
		case "has":
			if types[1] != "string" {
				return "", err
			}
			return "boolean", nil
		case "tier":
			if types[1] != "number" {
				return "", err
			}
			return "number", nil
		default:
			for _, typ := range types {
				if typ != "number" {
					return "", err
				}
			}
			return "number", nil
		}
	}
	return "", err
}

// UsedAttributes shares the validated, unoptimized plan with execution.
func UsedAttributes(expression string) map[string]bool {
	plan, err := CompileFromCache(expression)
	if err != nil {
		return nil
	}
	result := map[string]bool{}
	var collect func(*operation)
	collect = func(node *operation) {
		if node.kind == "call" && (node.text == "attr" || node.text == "attr_num") {
			result[node.args[0].text] = true
		}
		for _, child := range node.args {
			collect(child)
		}
	}
	collect(plan.root)
	return result
}
