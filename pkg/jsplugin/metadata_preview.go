package jsplugin

import (
	"fmt"
	"math"
	"reflect"

	"github.com/grafana/sobek/ast"
	"github.com/grafana/sobek/file"
	"github.com/grafana/sobek/parser"
	"github.com/grafana/sobek/token"
)

// PreviewMetadata reads literals and local constants whose containers cannot
// escape or be mutated. Calls, computed properties and getters stay unresolved.
// No module instance is created and no plugin JavaScript is executed.
func PreviewMetadata(source string) (map[string]any, []string, error) {
	program, err := parser.ParseFile(nil, "preview.js", source, 0, parser.IsModule, parser.WithDisableSourceMaps)
	if err != nil {
		return nil, nil, fmt.Errorf("plugin syntax is invalid: %w", err)
	}
	var object *ast.ObjectLiteral
	for _, entry := range program.Body {
		export, ok := entry.(*ast.ExportDeclaration)
		if !ok || export.LexicalDeclaration == nil || export.LexicalDeclaration.Token != token.CONST {
			continue
		}
		for _, binding := range export.LexicalDeclaration.List {
			identifier, ok := binding.Target.(*ast.Identifier)
			if ok && identifier.Name.String() == "meta" {
				object, _ = binding.Initializer.(*ast.ObjectLiteral)
			}
		}
	}
	if object == nil {
		return nil, nil, fmt.Errorf("preview requires an exported literal meta object")
	}
	constants, metaUnsafe := previewConstants(program, object)
	if metaUnsafe {
		return map[string]any{}, []string{"meta"}, nil
	}
	result := map[string]any{}
	unresolved := []string{}
	// A spread or computed property can overwrite even literal identity
	// fields. Do not present any field as resolved in that case.
	for _, property := range object.Value {
		keyed, ok := property.(*ast.PropertyKeyed)
		if !ok || keyed.Computed {
			return result, []string{"meta"}, nil
		}
	}
	for _, property := range object.Value {
		keyed, ok := property.(*ast.PropertyKeyed)
		if !ok || keyed.Computed {
			unresolved = append(unresolved, "computed/spread/getter")
			continue
		}
		name, ok := literalPropertyName(keyed.Key)
		if !ok {
			unresolved = append(unresolved, "computed")
			continue
		}
		if keyed.Kind != ast.PropertyKindValue {
			delete(result, name)
			unresolved = append(unresolved, name)
			continue
		}
		budget := 4096
		value, ok := previewLiteral(keyed.Value, 0, &budget, constants)
		if !ok {
			delete(result, name)
			unresolved = append(unresolved, name)
			continue
		}
		result[name] = value
	}
	return result, unresolved, nil
}

func literalPropertyName(value ast.Expression) (string, bool) {
	switch item := value.(type) {
	case *ast.Identifier:
		return item.Name.String(), true
	case *ast.StringLiteral:
		return item.Value.String(), true
	}
	return "", false
}

func previewLiteral(expression ast.Expression, depth int, budget *int, constants map[string]ast.Expression) (any, bool) {
	*budget--
	if depth > 32 || *budget < 0 {
		return nil, false
	}
	switch value := expression.(type) {
	case *ast.Identifier:
		initializer, ok := constants[value.Name.String()]
		if !ok || initializer.Idx0() >= value.Idx0() {
			return nil, false
		}
		return previewLiteral(initializer, depth+1, budget, constants)
	case *ast.StringLiteral:
		return value.Value.String(), true
	case *ast.NumberLiteral:
		switch number := value.Value.(type) {
		case int64:
			return number, true
		case float64:
			return number, !math.IsNaN(number) && !math.IsInf(number, 0)
		case int:
			return number, true
		}
	case *ast.BooleanLiteral:
		return value.Value, true
	case *ast.NullLiteral:
		return nil, true
	case *ast.ArrayLiteral:
		result := make([]any, 0, len(value.Value))
		for _, item := range value.Value {
			parsed, ok := previewLiteral(item, depth+1, budget, constants)
			if !ok {
				return nil, false
			}
			result = append(result, parsed)
		}
		return result, true
	case *ast.ObjectLiteral:
		result := map[string]any{}
		for _, item := range value.Value {
			property, ok := item.(*ast.PropertyKeyed)
			if !ok || property.Computed || property.Kind != ast.PropertyKindValue {
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

func previewConstants(program *ast.Program, meta *ast.ObjectLiteral) (map[string]ast.Expression, bool) {
	constants := map[string]ast.Expression{}
	definitions := map[string]file.Idx{}
	owners := map[file.Idx]string{}
	dependencies := map[string]map[string]bool{}
	unsafe := map[string]bool{}
	for _, statement := range program.Body {
		declaration, ok := statement.(*ast.LexicalDeclaration)
		if !ok || declaration.Token != token.CONST {
			continue
		}
		for _, binding := range declaration.List {
			identifier, ok := binding.Target.(*ast.Identifier)
			if !ok || binding.Initializer == nil {
				continue
			}
			name := identifier.Name.String()
			if constants[name] != nil {
				unsafe[name] = true
			}
			constants[name], definitions[name] = binding.Initializer, identifier.Idx
			owners[binding.Initializer.Idx0()] = name
			dependencies[name] = map[string]bool{}
		}
	}
	previewWalkAST(reflect.ValueOf(program), nil, func(node any, parents []any) bool {
		if exported, ok := node.(*ast.ExportSpecifier); ok {
			unsafe[exported.IdentifierName.String()] = true
			return false
		}
		identifier, ok := node.(*ast.Identifier)
		if !ok {
			return true
		}
		name := identifier.Name.String()
		if name != "meta" && constants[name] == nil {
			return false
		}
		if len(parents) == 0 {
			return false
		}
		parent := parents[len(parents)-1]
		if property, ok := parent.(*ast.PropertyKeyed); ok && property.Key == identifier && !property.Computed {
			return false
		}
		if dot, ok := parent.(*ast.DotExpression); ok && &dot.Identifier == identifier {
			return false
		}
		if binding, ok := parent.(*ast.Binding); ok && binding.Target == identifier {
			if name == "meta" && binding.Initializer == meta {
				return false
			}
			if definitions[name] != identifier.Idx {
				unsafe[name] = true
			}
			return false
		}
		var container ast.Node = identifier
		for i := len(parents) - 1; i >= 0; i-- {
			switch enclosing := parents[i].(type) {
			case *ast.ArrayLiteral:
				container = enclosing
			case *ast.PropertyKeyed:
				if enclosing.Value != container || i == 0 {
					break
				}
				object, ok := parents[i-1].(*ast.ObjectLiteral)
				if !ok {
					break
				}
				container = object
				i--
			default:
				i = -1
			}
		}
		if container == meta {
			return false
		}
		if owner := owners[container.Idx0()]; owner != "" {
			dependencies[owner][name] = true
			return false
		}
		if _, array := constants[name].(*ast.ArrayLiteral); array {
			if dot, ok := parent.(*ast.DotExpression); ok && dot.Left == identifier && len(parents) > 1 {
				if call, ok := parents[len(parents)-2].(*ast.CallExpression); ok && call.Callee == dot {
					switch dot.Identifier.Name.String() {
					case "includes", "indexOf", "lastIndexOf":
						return false
					}
				}
			}
		}
		unsafe[name] = true
		return false
	})
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

// The parser's AST has no public visitor. Traverse syntax fields without
// consulting runtime values; DeclarationList only duplicates existing nodes.
func previewWalkAST(value reflect.Value, parents []any, visit func(any, []any) bool) {
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Interface {
		if !value.IsNil() {
			previewWalkAST(value.Elem(), parents, visit)
		}
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() || value.Elem().Kind() != reflect.Struct {
			return
		}
		if !visit(value.Interface(), parents) {
			return
		}
		parents = append(append([]any(nil), parents...), value.Interface())
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		if value.Type().PkgPath() != "github.com/grafana/sobek/ast" {
			return
		}
		for i := 0; i < value.NumField(); i++ {
			if value.Type().Field(i).Name == "DeclarationList" {
				continue
			}
			child := value.Field(i)
			if child.Kind() == reflect.Struct && child.CanAddr() {
				child = child.Addr()
			}
			previewWalkAST(child, parents, visit)
		}
	} else if value.Kind() == reflect.Slice {
		for i := 0; i < value.Len(); i++ {
			previewWalkAST(value.Index(i), parents, visit)
		}
	}
}
