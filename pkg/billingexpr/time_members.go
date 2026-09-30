package billingexpr

import (
	"reflect"
	"time"

	"github.com/expr-lang/expr/ast"
)

const timeMembersFunction = "_billing_time_members"

// Preserve the MemberNode itself, including optional-chain short circuiting.
// Only its receiver is adapted; dynamic member names are still evaluated once.
type timeMemberPatcher struct{}

func (*timeMemberPatcher) Visit(node *ast.Node) {
	member, ok := (*node).(*ast.MemberNode)
	if !ok {
		return
	}
	if property, literal := member.Property.(*ast.StringNode); literal && property.Value != "Local" && property.Value != "GoString" {
		return
	}
	if call, ok := member.Node.(*ast.CallNode); ok {
		if name, ok := call.Callee.(*ast.IdentifierNode); ok && name.Value == timeMembersFunction {
			return
		}
	}
	member.Node = &ast.CallNode{Callee: &ast.IdentifierNode{Value: timeMembersFunction}, Arguments: []ast.Node{member.Node}}
}

func (e *Evaluation) timeMembers(value any) any {
	at, ok := value.(time.Time)
	if !ok {
		return value
	}
	methods := reflect.ValueOf(at)
	members := make(map[string]any, methods.NumMethod())
	for i := range methods.NumMethod() {
		members[methods.Type().Method(i).Name] = methods.Method(i).Interface()
	}
	members["Local"] = func() (time.Time, error) {
		value, err := e.invoke("time.Local", []any{at}, func() (any, error) { return at.Local(), nil })
		if err != nil {
			return time.Time{}, err
		}
		return value.(time.Time), nil
	}
	members["GoString"] = func() (string, error) {
		value, err := e.invoke("time.GoString", []any{at}, func() (any, error) { return at.GoString(), nil })
		if err != nil {
			return "", err
		}
		return value.(string), nil
	}
	return members
}
