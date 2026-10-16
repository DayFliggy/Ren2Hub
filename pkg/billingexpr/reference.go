package billingexpr

import (
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// ReferenceExpressionReason only limits automatic catalog examples. Arbitrary
// expressions retain their existing execution and administrator-preview semantics.
// Inspect the unoptimized tree, including unselected branches: no clock, request
// context, dynamic access or environment-dependent builtin may run in a catalog.
func ReferenceExpressionReason(expression string) string {
	if len(expression) > 65536 {
		return "unsupported_expression"
	}
	_, body := ParseExprVersion(expression)
	tree, err := parser.Parse(body)
	if err != nil {
		return "unsupported_expression"
	}
	return referenceNodeReason(tree.Node, 0)
}

func referenceNodeReason(node ast.Node, depth int) string {
	if depth > 128 {
		return "unsupported_expression"
	}
	var children []ast.Node
	switch n := node.(type) {
	case *ast.NilNode, *ast.IntegerNode, *ast.FloatNode, *ast.StringNode, *ast.BoolNode:
		return ""
	case *ast.UnaryNode:
		switch n.Operator {
		case "+", "-", "!", "not":
		default:
			return "unsupported_expression"
		}
		children = []ast.Node{n.Node}
	case *ast.BinaryNode:
		switch n.Operator {
		case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=", "&&", "||", "and", "or":
		default:
			return "unsupported_expression"
		}
		children = []ast.Node{n.Left, n.Right}
	case *ast.ConditionalNode:
		children = []ast.Node{n.Cond, n.Exp1, n.Exp2}
	case *ast.BuiltinNode:
		switch n.Name {
		case "min", "max", "abs", "ceil", "floor":
		default:
			return "unsupported_expression"
		}
		children = n.Arguments
	case *ast.CallNode:
		callee, ok := n.Callee.(*ast.IdentifierNode)
		if !ok {
			return "dynamic_context"
		}
		switch callee.Value {
		case "param", "header", "hour", "minute", "weekday", "month", "day", "now", "date", "timezone":
			return "dynamic_context"
		case "u", "tier":
			if len(n.Arguments) == 0 {
				return "unsupported_expression"
			}
			if _, ok := n.Arguments[0].(*ast.StringNode); !ok {
				return "dynamic_context"
			}
		case "min", "max", "abs", "ceil", "floor":
		default:
			return "unsupported_expression"
		}
		children = n.Arguments
	default:
		return "unsupported_expression"
	}
	for _, child := range children {
		if reason := referenceNodeReason(child, depth+1); reason != "" {
			return reason
		}
	}
	return ""
}
