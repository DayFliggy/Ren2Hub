package billingexpr

import (
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/shopspring/decimal"
)

// StaticReferencePrices exposes unconditional token/request prices. Branches,
// request multipliers and usage-dependent coefficients have no single unit price.
// This is a display projection; evaluation and settlement remain unchanged.
func StaticReferencePrices(expression string) (map[string]string, bool) {
	if _, err := CompileFromCache(expression); err != nil {
		return nil, false
	}
	_, body := ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil {
		return nil, false
	}
	node := tree.Node
	if call, ok := node.(*ast.CallNode); ok && callName(call) == "tier" && len(call.Arguments) == 2 {
		node = call.Arguments[1]
	}
	if call, ok := node.(*ast.CallNode); ok && callName(call) == "fixed" && len(call.Arguments) == 1 {
		value, ok := referenceLiteral(body, call.Arguments[0])
		if !ok {
			return nil, false
		}
		return map[string]string{"request": value.String()}, true
	}
	values := map[string]decimal.Decimal{}
	if !readReferenceTerms(body, node, values) {
		return nil, false
	}
	result := map[string]string{}
	for variable, key := range map[string]string{
		"p": "input", "c": "output", "cr": "cache", "cc": "cache_create",
		"cc1h": "cache_create_1h", "img": "image", "img_cr": "image_cache",
		"img_o": "image_output", "ai": "audio_input", "ao": "audio_output",
	} {
		if value, ok := values[variable]; ok {
			result[key] = value.String()
		}
	}
	return result, len(result) > 0
}

func callName(call *ast.CallNode) string {
	if id, ok := call.Callee.(*ast.IdentifierNode); ok {
		return id.Value
	}
	return ""
}

func referenceLiteral(body string, node ast.Node) (decimal.Decimal, bool) {
	switch node.(type) {
	case *ast.IntegerNode, *ast.FloatNode:
	default:
		return decimal.Zero, false
	}
	loc := node.Location()
	runes := []rune(body)
	if loc.From < 0 || loc.To > len(runes) || loc.From >= loc.To {
		return decimal.Zero, false
	}
	value, err := decimal.NewFromString(string(runes[loc.From:loc.To]))
	return value, err == nil && !value.IsNegative() && value.Exponent() >= -30 && value.Exponent() <= 18
}

func readReferenceTerms(body string, node ast.Node, values map[string]decimal.Decimal) bool {
	binary, ok := node.(*ast.BinaryNode)
	if !ok {
		return false
	}
	if binary.Operator == "+" {
		return readReferenceTerms(body, binary.Left, values) && readReferenceTerms(body, binary.Right, values)
	}
	if binary.Operator != "*" {
		return false
	}
	id, ok := binary.Left.(*ast.IdentifierNode)
	literal := binary.Right
	if !ok {
		id, ok = binary.Right.(*ast.IdentifierNode)
		literal = binary.Left
	}
	if !ok {
		return false
	}
	switch id.Value {
	case "p", "c", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao":
	default:
		return false
	}
	value, ok := referenceLiteral(body, literal)
	if !ok {
		return false
	}
	values[id.Value] = values[id.Value].Add(value)
	return true
}
