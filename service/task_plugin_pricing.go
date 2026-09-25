package service

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/shopspring/decimal"
)

type TaskPluginPriceCandidate struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type TaskPluginPriceSchema struct {
	Model          string                               `json:"model"`
	PluginKey      string                               `json:"plugin_key"`
	PluginVersion  string                               `json:"plugin_version,omitempty"`
	SourceHash     string                               `json:"source_hash,omitempty"`
	UsageSchema    *billingexpr.UsageSchema             `json:"usage_schema"`
	ManifestSchema map[string]jsplugin.UsageFieldSchema `json:"manifest_schema"`
	ReviewRequired bool                                 `json:"review_required"`
	Plugins        []TaskPluginPriceCandidate           `json:"plugins"`
}

// ResolveTaskPluginPriceSchema never chooses one provider's metadata for a
// shared model. Only the reviewed, unchanged factory source supplies a default
// fact-origin contract; custom plugins require an explicitly reviewed schema.
func ResolveTaskPluginPriceSchema(modelName, pluginKey string) (*TaskPluginPriceSchema, error) {
	modelName, pluginKey = strings.TrimSpace(modelName), strings.TrimSpace(pluginKey)
	if modelName == "" {
		return nil, fmt.Errorf("model is required")
	}
	result := &TaskPluginPriceSchema{Model: modelName, Plugins: []TaskPluginPriceCandidate{}}
	generation := jsplugin.DefaultRegistry.Generation()
	declared, exists := generation.CanonicalModel(modelName)
	if !exists && model.DB != nil {
		if alias, ok := model.ResolveTaskModelAlias(generation, modelName); ok {
			if alias.Declared == "" {
				return nil, fmt.Errorf("task model alias has ambiguous declared models")
			}
			declared, exists = alias.Declared, true
		}
	}
	if !exists {
		if pluginKey != "" {
			return nil, fmt.Errorf("selected plugin does not declare this model")
		}
		return result, nil
	}
	var selected *jsplugin.LoadedPlugin
	candidates := generation.PluginsByModel(declared)
	for _, candidate := range candidates {
		result.Plugins = append(result.Plugins, TaskPluginPriceCandidate{Key: candidate.Meta.Key, Name: candidate.Meta.Name, Version: candidate.Meta.Version})
		if candidate.Meta.Key == pluginKey || (pluginKey == "" && len(candidates) == 1) {
			selected = candidate
		}
	}
	if selected == nil {
		if pluginKey != "" {
			return nil, fmt.Errorf("selected plugin does not declare this model")
		}
		result.ReviewRequired = true
		return result, nil
	}
	result.PluginKey, result.PluginVersion, result.SourceHash = selected.Meta.Key, selected.Meta.Version, selected.SourceHash
	manifest, _ := selected.Meta.UsageForModel(declared)
	result.ManifestSchema = jsplugin.CloneUsageSchema(manifest)
	source, err := plugins.Source(selected.Meta.Key)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256([]byte(source))) != selected.SourceHash {
		result.ReviewRequired = true
		return result, nil
	}
	price, err := plugins.FrozenTaskPriceVersion(selected.Meta, declared, "v2:0")
	if err != nil {
		return nil, fmt.Errorf("reviewed factory usage contract is unavailable: %w", err)
	}
	result.UsageSchema = price.UsageSchema
	return result, nil
}

// ValidateTaskPluginPricePublication runs only for new publications. Reads and
// in-flight quotes retain their original schema even after plugin activation.
func ValidateTaskPluginPricePublication(price billing.PriceVersion, pluginKey string) (billing.PriceVersion, error) {
	current, err := ResolveTaskPluginPriceSchema(price.Model, pluginKey)
	if err != nil {
		return price, err
	}
	if len(current.Plugins) == 0 {
		return price, nil
	}
	if current.PluginKey == "" {
		return price, fmt.Errorf("model is declared by multiple task plugins; plugin_key is required")
	}
	if price.UsageSchema == nil {
		// Fixed call prices have no provider-dependent quantity. Other paid
		// dimensions still require a reviewed schema and completion evidence.
		if price.Mode == billing.ModeFree || (price.Mode == billing.ModeUnitPrice && len(price.Prices) == 1 &&
			price.Prices[0].Quantity == "calls" && price.Prices[0].Unit == "per_call") {
			price.ID, price.CreatedAt, price.Hash = 0, 0, ""
			return billing.PreparePriceVersion(price)
		}
		return price, fmt.Errorf("task plugin price requires an explicitly reviewed usage_schema")
	}
	schema, err := billingexpr.FreezeUsageSchema(price.UsageSchema)
	if err != nil {
		return price, err
	}
	for name, field := range schema.Fields {
		manifest, exists := current.ManifestSchema[name]
		if !exists {
			return price, fmt.Errorf("usage field %q is no longer declared by the selected plugin", name)
		}
		fieldType := manifest.Type
		if len(manifest.Enum) > 0 {
			fieldType = "string"
			if len(field.Enum) == 0 {
				return price, fmt.Errorf("usage field %q requires the current plugin enum", name)
			}
			for _, value := range field.Enum {
				if !slices.Contains(manifest.Enum, value) {
					return price, fmt.Errorf("usage field %q enum value %q is not supported by the current plugin", name, value)
				}
			}
		}
		if field.Type != fieldType || field.Unit != manifest.Unit {
			return price, fmt.Errorf("usage field %q type or unit differs from the current plugin", name)
		}
		if current.UsageSchema != nil {
			reviewed, exists := current.UsageSchema.Fields[name]
			if !exists || field.Source != reviewed.Source || (reviewed.Integer && !field.Integer) || !usageBoundsWithin(field, reviewed) {
				return price, fmt.Errorf("usage field %q differs from the reviewed fact source or bounds", name)
			}
		}
	}
	price.UsageSchema = schema
	price.ID, price.CreatedAt, price.Hash = 0, 0, ""
	price, err = billing.PreparePriceVersion(price)
	if err != nil {
		return price, err
	}
	if price.Mode == billing.ModeExpression {
		if err := validateTaskPriceEnumLiterals(price); err != nil {
			return price, err
		}
	}
	if current.UsageSchema != nil {
		if err := plugins.ValidateFactoryPriceReservation(current.PluginKey, price); err != nil {
			return price, err
		}
	}
	return price, nil
}

func usageBoundsWithin(field, reviewed billingexpr.UsageField) bool {
	if field.Type != "number" {
		return true
	}
	bound := func(value, fallback string) decimal.Decimal {
		if value == "" {
			value = fallback
		}
		parsed, _ := decimal.NewFromString(value) // FreezeUsageSchema validated it.
		return parsed
	}
	return !bound(field.Minimum, "0").LessThan(bound(reviewed.Minimum, "0")) &&
		!bound(field.Maximum, "2147483647").GreaterThan(bound(reviewed.Maximum, "2147483647"))
}

func validateTaskPriceEnumLiterals(price billing.PriceVersion) error {
	_, expression := billingexpr.ParseExprVersion(price.Expression)
	tree, err := parser.Parse(expression)
	if err != nil {
		return err
	}
	usageField := func(node ast.Node) string {
		call, ok := node.(*ast.CallNode)
		if !ok || len(call.Arguments) != 1 {
			return ""
		}
		callee, ok := call.Callee.(*ast.IdentifierNode)
		if !ok || callee.Value != "u" {
			return ""
		}
		name, ok := call.Arguments[0].(*ast.StringNode)
		if !ok {
			return ""
		}
		return name.Value
	}
	check := func(name string, node ast.Node) {
		field := price.UsageSchema.Fields[name]
		if len(field.Enum) == 0 {
			return
		}
		if literal, ok := node.(*ast.StringNode); ok && !slices.Contains(field.Enum, literal.Value) {
			err = fmt.Errorf("expression uses unsupported enum value %q for usage field %q", literal.Value, name)
		}
	}
	// Inspect the unoptimized tree so even currently unreachable price branches
	// cannot retain a removed plugin enum value in a newly published definition.
	ast.Find(tree.Node, func(node ast.Node) bool {
		binary, ok := node.(*ast.BinaryNode)
		if !ok {
			return false
		}
		switch binary.Operator {
		case "==", "!=":
			check(usageField(binary.Left), binary.Right)
			check(usageField(binary.Right), binary.Left)
		case "in":
			if values, ok := binary.Right.(*ast.ArrayNode); ok {
				for _, value := range values.Nodes {
					check(usageField(binary.Left), value)
				}
			}
		}
		return err != nil
	})
	return err
}
