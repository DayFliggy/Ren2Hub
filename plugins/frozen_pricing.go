package plugins

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// FrozenTaskPriceVersion converts an explicitly supplied, reviewed task price.
// Factory metadata contains capabilities and usage examples, never prices.
// Missing prices and fields without billable completion evidence stay unpriced.
func FrozenTaskPriceVersion(meta jsplugin.Meta, model, expression string) (billing.PriceVersion, error) {
	if strings.TrimSpace(expression) == "" {
		return billing.PriceVersion{}, fmt.Errorf("factory metadata has no price; an explicit reviewed expression is required")
	}
	factorySource, err := Source(meta.Key)
	if err != nil {
		return billing.PriceVersion{}, fmt.Errorf("unsupported factory usage contract: %w", err)
	}
	factory, err := jsplugin.CompilePlugin(factorySource, jsplugin.Options{Key: meta.Key})
	if err != nil {
		return billing.PriceVersion{}, err
	}
	if factory.Meta.Version != meta.Version {
		return billing.PriceVersion{}, fmt.Errorf("factory usage contract version mismatch")
	}
	if !slices.Contains(factory.Meta.Models, model) {
		return billing.PriceVersion{}, fmt.Errorf("model is not declared by this factory plugin")
	}
	fields, _ := factory.Meta.UsageForModel(model)
	schema := &billingexpr.UsageSchema{Version: billingexpr.UsageSchemaVersion, Fields: map[string]billingexpr.UsageField{}}
	for key, field := range fields {
		source := factoryUsageSource(meta.Key, model, key)
		converted := billingexpr.UsageField{Source: source, Type: field.Type, Unit: field.Unit, Enum: append([]string(nil), field.Enum...)}
		if len(field.Enum) > 0 {
			converted.Type = "string"
			converted.Unit = ""
		}
		if converted.Type == "number" {
			converted.Minimum = "0"
			switch field.Unit {
			case "count":
				converted.Maximum = strconv.Itoa(dto.MaxImageN)
				converted.Integer = true
			case "second":
				converted.Maximum = strconv.Itoa(relaycommon.MaxTaskDurationSeconds)
			case "token":
				converted.Maximum = strconv.FormatInt(common.MaxQuota, 10)
				converted.Integer = true
			case "credit":
				converted.Maximum = strconv.FormatInt(common.MaxQuota, 10)
			default:
				return billing.PriceVersion{}, fmt.Errorf("unreviewed usage unit %q", field.Unit)
			}
		}
		schema.Fields[key] = converted
	}
	schema, err = billingexpr.FreezeUsageSchema(schema)
	if err != nil {
		return billing.PriceVersion{}, err
	}
	expression = strings.TrimSpace(expression)
	if !strings.HasPrefix(expression, "v2:") {
		expression = "v2:" + expression
	}
	used, err := billingexpr.UsedUsageFields(expression, schema)
	if err != nil {
		return billing.PriceVersion{}, err
	}
	for key := range used {
		field := schema.Fields[key]
		if field.Source == billingexpr.UsageEstimateOnly {
			return billing.PriceVersion{}, fmt.Errorf("%s/%s has no proven completion fact for %s", meta.Key, model, key)
		}
		field.Required = true
		schema.Fields[key] = field
	}
	schema.Hash = ""
	schema, err = billingexpr.FreezeUsageSchema(schema)
	if err != nil {
		return billing.PriceVersion{}, err
	}
	price, err := billing.PreparePriceVersion(billing.PriceVersion{Model: model, Mode: billing.ModeExpression, Expression: expression, UsageSchema: schema})
	if err != nil {
		return billing.PriceVersion{}, err
	}
	if err := ValidateFactoryPriceReservation(meta.Key, price); err != nil {
		return billing.PriceVersion{}, err
	}
	return price, nil
}

func factoryUsageSource(key, model, field string) string {
	switch key {
	case "google", "vertex-ai", "jimeng":
		return billingexpr.UsageEstimateOnly
	case "alibaba":
		if field == "prompt_extend" {
			return billingexpr.UsageRequestFixed
		}
	case "doubao":
		if field == "video_input" || field == "generate_audio" || field == "layer_decomposition" {
			return billingexpr.UsageRequestFixed
		}
	case "hailuo":
		if model != "MiniMax-H3" && field == "seconds" {
			return billingexpr.UsageEstimateOnly
		}
	case "vidu":
		if field != "credits" {
			return billingexpr.UsageEstimateOnly
		}
	}
	return billingexpr.UsageCompletionFact
}
