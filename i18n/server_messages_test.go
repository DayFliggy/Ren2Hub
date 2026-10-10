package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serverMessageDir holds the web console translations of the English source
// text the backend sends as message_key, one flat JSON map per stream.
const serverMessageDir = "../frontend/src/i18n/locales/zh-CN/server-messages"

const commonImportPath = "github.com/QuantumNous/new-api/common"

// messageKeyFuncs lists the common functions that send their key argument to
// the web console as message_key, with the positions of the key and params.
var messageKeyFuncs = map[string]struct{ key, params int }{
	"NewMessage":  {key: 0, params: 1},
	"ApiErrorT":   {key: 1, params: 2},
	"ApiSuccessT": {key: 1, params: 3},
}

var placeholderPattern = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

type messageKeyUse struct {
	key      string
	position string
	params   []string // keys of a literal params map; nil when not literal
}

// Every web console message key the backend sends must have a Simplified
// Chinese translation with the same {{name}} placeholders, and a key must be a
// string literal (or a same-package string constant) so this test can see it.
func TestServerMessageCatalog(t *testing.T) {
	catalog := loadServerMessageCatalog(t)

	uses, problems := collectMessageKeyUses(t, "..")
	for _, problem := range problems {
		t.Error(problem)
	}
	for _, use := range uses {
		translation, ok := catalog[use.key]
		if !assert.True(t, ok, "%s: message key %q has no translation in %s", use.position, use.key, serverMessageDir) {
			continue
		}
		assert.Equal(t, placeholders(use.key), placeholders(translation), "%s: placeholders of the translation of %q", use.position, use.key)
		if use.params != nil {
			assert.Equal(t, placeholders(use.key), use.params, "%s: params of %q", use.position, use.key)
		}
	}
	// Fail reasons are matched exactly, so they take no params.
	for _, reason := range constant.TaskFailReasons {
		_, ok := catalog[reason]
		if assert.True(t, ok, "task fail reason %q has no translation in %s", reason, serverMessageDir) {
			assert.Empty(t, placeholders(reason), "task fail reason %q", reason)
		}
	}
	for key, translation := range catalog {
		assert.Equal(t, placeholders(key), placeholders(translation), "placeholders of the translation of %q", key)
	}
}

// The scanner behind TestServerMessageCatalog finds keys under any import
// name, resolves constants, reads the params of each helper at its own
// position, skips the helpers forwarding their key, ignores the relay/common
// package of the same name, and reports keys it cannot read.
func TestCollectMessageKeyUses(t *testing.T) {
	root := t.TempDir()
	write := func(path, source string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(source), 0o644))
	}
	write("common/message.go", `package common
func NewMessage(key string, params ...map[string]any) error { return nil }
func ApiErrorT(c any, key string, params ...map[string]any) { NewMessage(key, params...) }
var ErrInCommon = NewMessage("In common {{a}}", map[string]any{"a": 1})
`)
	write("controller/handler.go", `package controller
import c2 "github.com/QuantumNous/new-api/common"
const constKey = "Const key"
func handle(text string, params map[string]any) {
	c2.NewMessage("Literal")
	c2.NewMessage(constKey)
	c2.ApiSuccessT(nil, "Saved {{n}}", nil, map[string]any{"n": 1})
	c2.ApiErrorT(nil, "Forwarded {{x}}", params)
	c2.NewMessage(text)
}
`)
	write("controller/handler_test.go", `package controller
import "github.com/QuantumNous/new-api/common"
func helper() { common.NewMessage("Only in a test") }
`)
	write("relay/common/other.go", `package common
func NewMessage(key string) {}
func other() { NewMessage("Not the common package") }
`)

	uses, problems := collectMessageKeyUses(t, root)
	got := map[string][]string{}
	for _, use := range uses {
		got[use.key] = use.params
	}
	assert.Equal(t, map[string][]string{
		"In common {{a}}": {"a"},
		"Literal":         {},
		"Const key":       {},
		"Saved {{n}}":     {"n"},
		"Forwarded {{x}}": nil,
	}, got)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "handler.go:9")
}

// loadServerMessageCatalog merges the JSON maps; a key may live in one file only.
func loadServerMessageCatalog(t *testing.T) map[string]string {
	files, err := filepath.Glob(filepath.Join(serverMessageDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "no server message translations in %s", serverMessageDir)
	catalog := map[string]string{}
	owner := map[string]string{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		var messages map[string]string
		require.NoError(t, common.Unmarshal(data, &messages), file)
		for key, translation := range messages {
			if previous, ok := owner[key]; ok {
				t.Errorf("message key %q is translated in both %s and %s", key, previous, filepath.Base(file))
			}
			assert.NotEmpty(t, strings.TrimSpace(translation), "%s: empty translation of %q", filepath.Base(file), key)
			catalog[key] = translation
			owner[key] = filepath.Base(file)
		}
	}
	return catalog
}

func placeholders(text string) []string {
	names := []string{}
	for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(names, match[1]) {
			names = append(names, match[1])
		}
	}
	sort.Strings(names)
	return names
}

// collectMessageKeyUses parses the non-test Go files of the root module and
// returns every message key passed to a messageKeyFuncs function.
func collectMessageKeyUses(t *testing.T, root string) ([]messageKeyUse, []string) {
	fset := token.NewFileSet()
	files := map[string][]*ast.File{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "frontend" || name == "node_modules" || name == "relaykit" || name == "tokenkit") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files[filepath.Dir(path)] = append(files[filepath.Dir(path)], file)
		return nil
	})
	require.NoError(t, err)

	var uses []messageKeyUse
	var problems []string
	commonDir := filepath.Join(root, "common")
	for dir, pkgFiles := range files {
		constants := stringConstants(pkgFiles)
		inCommon := dir == commonDir
		for _, file := range pkgFiles {
			qualifier := commonQualifier(file)
			if qualifier == "" && !inCommon {
				continue
			}
			for _, decl := range file.Decls {
				fn, _ := decl.(*ast.FuncDecl)
				ast.Inspect(decl, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					name := calledCommonFunction(call, qualifier, inCommon)
					args, emits := messageKeyFuncs[name]
					if !emits || len(call.Args) <= args.key {
						return true
					}
					position := fset.Position(call.Pos()).String()
					keyArg := call.Args[args.key]
					if inCommon && forwardsKeyParameter(fn, keyArg) {
						return true
					}
					key, ok := stringValue(keyArg, constants)
					if !ok {
						problems = append(problems, fmt.Sprintf("%s: the key passed to %s must be a string literal or constant", position, name))
						return true
					}
					var params []ast.Expr
					if len(call.Args) > args.params {
						params = call.Args[args.params:]
					}
					use := messageKeyUse{key: key, position: position}
					if call.Ellipsis == token.NoPos {
						use.params = literalParamNames(params)
					}
					uses = append(uses, use)
					return true
				})
			}
		}
	}
	return uses, problems
}

// commonQualifier returns the name a file imports the common package under,
// or "" when it does not import it.
func commonQualifier(file *ast.File) string {
	for _, spec := range file.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		if path != commonImportPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return "common"
	}
	return ""
}

func calledCommonFunction(call *ast.CallExpr, qualifier string, inCommon bool) string {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		if ident, ok := fun.X.(*ast.Ident); ok && qualifier != "" && ident.Name == qualifier {
			return fun.Sel.Name
		}
	case *ast.Ident:
		if inCommon {
			return fun.Name
		}
	}
	return ""
}

// forwardsKeyParameter reports a call inside a messageKeyFuncs function that
// passes its own key parameter on, such as ApiErrorT calling NewMessage.
func forwardsKeyParameter(fn *ast.FuncDecl, arg ast.Expr) bool {
	ident, ok := arg.(*ast.Ident)
	if !ok || fn == nil || fn.Recv != nil {
		return false
	}
	if _, emits := messageKeyFuncs[fn.Name.Name]; !emits {
		return false
	}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name.Name == ident.Name {
				return true
			}
		}
	}
	return false
}

func stringConstants(files []*ast.File) map[string]string {
	constants := map[string]string{}
	for _, file := range files {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					if i >= len(value.Values) {
						continue
					}
					if text, ok := stringValue(value.Values[i], nil); ok {
						constants[name.Name] = text
					}
				}
			}
		}
	}
	return constants
}

func stringValue(expr ast.Expr, constants map[string]string) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		return text, err == nil
	case *ast.Ident:
		text, ok := constants[value.Name]
		return text, ok
	}
	return "", false
}

// literalParamNames returns the sorted keys of the params: none when no params
// are passed, nil when they are not a map literal with string keys.
func literalParamNames(args []ast.Expr) []string {
	if len(args) == 0 {
		return []string{}
	}
	if len(args) != 1 {
		return nil
	}
	literal, ok := args[0].(*ast.CompositeLit)
	if !ok {
		return nil
	}
	names := []string{}
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return nil
		}
		name, ok := stringValue(pair.Key, nil)
		if !ok {
			return nil
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
