package jsplugin

import (
	"fmt"
	"math"

	"github.com/Calcium-Ion/moejs/syntax"
)

// PreviewMetadata reads literals and local constants whose containers cannot
// escape or be mutated. Calls, computed properties and getters stay unresolved.
// No module instance is created and no plugin JavaScript is executed.
func PreviewMetadata(source string) (map[string]any, []string, error) {
	module, err := syntax.ParseModule("preview.js", source, syntax.Options{})
	if err != nil {
		return nil, nil, fmt.Errorf("plugin syntax is invalid: %w", err)
	}
	var object *syntax.ObjectLit
	for _, statement := range module.Body {
		export, ok := statement.(*syntax.ExportDecl)
		if !ok {
			continue
		}
		declaration, ok := export.Decl.(*syntax.VarDecl)
		if !ok || declaration.Kind != syntax.DeclConst {
			continue
		}
		for _, declarator := range declaration.Decls {
			identifier, ok := declarator.Target.(*syntax.Ident)
			if ok && identifier.Name == "meta" {
				object, _ = declarator.Init.(*syntax.ObjectLit)
			}
		}
	}
	if object == nil {
		return nil, nil, fmt.Errorf("preview requires an exported literal meta object")
	}
	constants, metaUnsafe := previewConstants(module, object)
	if metaUnsafe {
		return map[string]any{}, []string{"meta"}, nil
	}
	result := map[string]any{}
	unresolved := []string{}
	// A spread or computed property can overwrite even literal identity
	// fields. Do not present any field as resolved in that case.
	for _, property := range object.Props {
		if !previewKeyed(property) {
			return result, []string{"meta"}, nil
		}
	}
	for _, property := range object.Props {
		name, ok := literalPropertyName(property.Key)
		if !ok {
			unresolved = append(unresolved, "computed")
			continue
		}
		if property.Kind != syntax.PropInit {
			delete(result, name)
			unresolved = append(unresolved, name)
			continue
		}
		budget := 4096
		value, ok := previewLiteral(property.Value, 0, &budget, constants)
		if !ok {
			delete(result, name)
			unresolved = append(unresolved, name)
			continue
		}
		result[name] = value
	}
	return result, unresolved, nil
}

// previewKeyed reports a property with a literal key: key: value, a method,
// a getter or a setter. Spreads, shorthands and computed keys are not.
func previewKeyed(property *syntax.Property) bool {
	if property.Computed {
		return false
	}
	switch property.Kind {
	case syntax.PropInit, syntax.PropMethod, syntax.PropGetter, syntax.PropSetter:
		return true
	}
	return false
}

func literalPropertyName(value syntax.Expr) (string, bool) {
	switch item := value.(type) {
	case *syntax.Ident:
		return item.Name, true
	case *syntax.StringLit:
		return item.Value, true
	}
	return "", false
}

func previewLiteral(expression syntax.Expr, depth int, budget *int, constants map[string]syntax.Expr) (any, bool) {
	*budget--
	if depth > 32 || *budget < 0 {
		return nil, false
	}
	switch value := expression.(type) {
	case *syntax.Ident:
		initializer, ok := constants[value.Name]
		if !ok {
			return nil, false
		}
		if start, _ := initializer.Range(); start >= value.Pos {
			return nil, false
		}
		return previewLiteral(initializer, depth+1, budget, constants)
	case *syntax.StringLit:
		return value.Value, true
	case *syntax.NumberLit:
		number := value.Value
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, false
		}
		if math.Trunc(number) == number && math.Abs(number) <= float64(maxSafeInteger) {
			return int64(number), true
		}
		return number, true
	case *syntax.BoolLit:
		return value.Value, true
	case *syntax.NullLit:
		return nil, true
	case *syntax.ArrayLit:
		result := make([]any, 0, len(value.Elems))
		for _, item := range value.Elems {
			if item == nil {
				return nil, false
			}
			parsed, ok := previewLiteral(item, depth+1, budget, constants)
			if !ok {
				return nil, false
			}
			result = append(result, parsed)
		}
		return result, true
	case *syntax.ObjectLit:
		result := map[string]any{}
		for _, property := range value.Props {
			if property.Computed || property.Kind != syntax.PropInit {
				return nil, false
			}
			name, ok := literalPropertyName(property.Key)
			if !ok {
				return nil, false
			}
			parsed, ok := previewLiteral(property.Value, depth+1, budget, constants)
			if !ok {
				return nil, false
			}
			result[name] = parsed
		}
		return result, true
	}
	return nil, false
}

func previewConstants(module *syntax.Module, meta *syntax.ObjectLit) (map[string]syntax.Expr, bool) {
	constants := map[string]syntax.Expr{}
	definitions := map[string]*syntax.Ident{}
	owners := map[syntax.Node]string{}
	dependencies := map[string]map[string]bool{}
	unsafe := map[string]bool{}
	for _, statement := range module.Body {
		declaration, ok := statement.(*syntax.VarDecl)
		if !ok || declaration.Kind != syntax.DeclConst {
			continue
		}
		for _, declarator := range declaration.Decls {
			identifier, ok := declarator.Target.(*syntax.Ident)
			if !ok || declarator.Init == nil {
				continue
			}
			name := identifier.Name
			if constants[name] != nil {
				unsafe[name] = true
			}
			constants[name], definitions[name] = declarator.Init, identifier
			owners[declarator.Init] = name
			dependencies[name] = map[string]bool{}
		}
	}
	syntax.Walk(&previewVisitor{visit: func(node syntax.Node, parents []syntax.Node) bool {
		if exported, ok := node.(*syntax.ExportSpec); ok {
			unsafe[exported.Local.Name] = true
			return false
		}
		identifier, ok := node.(*syntax.Ident)
		if !ok {
			return true
		}
		name := identifier.Name
		if name != "meta" && constants[name] == nil {
			return false
		}
		if len(parents) == 0 {
			return false
		}
		parent := parents[len(parents)-1]
		if previewPropertyName(parent, identifier) {
			return false
		}
		if declarator, ok := parent.(*syntax.Declarator); ok && declarator.Target == identifier {
			if name == "meta" && declarator.Init == meta {
				return false
			}
			if definitions[name] != identifier {
				unsafe[name] = true
			}
			return false
		}
		container := previewContainer(identifier, parents)
		if container == meta {
			return false
		}
		if owner := owners[container]; owner != "" {
			dependencies[owner][name] = true
			return false
		}
		if _, array := constants[name].(*syntax.ArrayLit); array && previewReadOnlyArrayCall(identifier, parents) {
			return false
		}
		unsafe[name] = true
		return false
	}}, module)
	for changed := true; changed; {
		changed = false
		for name := range unsafe {
			for dependency := range dependencies[name] {
				if !unsafe[dependency] {
					unsafe[dependency], changed = true, true
				}
			}
		}
	}
	for name := range unsafe {
		delete(constants, name)
	}
	return constants, unsafe["meta"]
}

// previewPropertyName reports an identifier that only names a property: a
// literal object or pattern key, or the name after a dot.
func previewPropertyName(parent syntax.Node, identifier *syntax.Ident) bool {
	switch node := parent.(type) {
	case *syntax.Property:
		return node.Key == identifier && !node.Computed && node.Kind != syntax.PropShorthand
	case *syntax.PatternProp:
		return node.Key == identifier && !node.Computed
	case *syntax.MemberExpr:
		return node.Prop == identifier && !node.Computed
	}
	return false
}

// previewContainer climbs from a reference through the array elements and
// key: value properties holding it to the outermost literal containing it.
func previewContainer(identifier *syntax.Ident, parents []syntax.Node) syntax.Node {
	var container syntax.Node = identifier
	for i := len(parents) - 1; i >= 0; i-- {
		switch enclosing := parents[i].(type) {
		case *syntax.ArrayLit:
			container = enclosing
		case *syntax.Property:
			// Shorthands and spreads hold the reference itself, not a copy.
			if enclosing.Value != container || enclosing.Kind != syntax.PropInit || i == 0 {
				return container
			}
			object, ok := parents[i-1].(*syntax.ObjectLit)
			if !ok {
				return container
			}
			container = object
			i--
		default:
			return container
		}
	}
	return container
}

// previewReadOnlyArrayCall reports NAME.includes(...), NAME.indexOf(...) or
// NAME.lastIndexOf(...), which read an array without changing or exposing it.
func previewReadOnlyArrayCall(identifier *syntax.Ident, parents []syntax.Node) bool {
	if len(parents) < 2 {
		return false
	}
	member, ok := parents[len(parents)-1].(*syntax.MemberExpr)
	if !ok || member.Object != identifier || member.Computed {
		return false
	}
	call, ok := parents[len(parents)-2].(*syntax.CallExpr)
	if !ok || call.Callee != member {
		return false
	}
	method, ok := member.Prop.(*syntax.Ident)
	if !ok {
		return false
	}
	switch method.Name {
	case "includes", "indexOf", "lastIndexOf":
		return true
	}
	return false
}

// previewVisitor walks the syntax tree with the chain of enclosing nodes;
// visit returns false to skip a node's children.
type previewVisitor struct {
	parents []syntax.Node
	visit   func(node syntax.Node, parents []syntax.Node) bool
}

func (v *previewVisitor) Visit(node syntax.Node) syntax.Visitor {
	if !v.visit(node, v.parents) {
		return nil
	}
	parents := append(append([]syntax.Node(nil), v.parents...), node)
	return &previewVisitor{parents: parents, visit: v.visit}
}
