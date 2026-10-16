package service

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/types"
)

func taskUsageReference(name, pluginKey, pluginName string, values model.PricingValues, manifest map[string]jsplugin.UsageFieldSchema, examples []jsplugin.UsageExample) *types.UsageReference {
	reference := &types.UsageReference{Status: "unavailable", PluginKey: pluginKey, PluginName: pluginName}
	if len(examples) == 0 {
		reference.Reason = "missing_example"
		return reference
	}
	example := jsplugin.CloneUsageExamples(examples[:1])[0]
	reference.Label, reference.Facts = example.Label, example.Facts
	expression, _ := values["billing_setting.billing_expr"].(string)
	if reason := billingexpr.ReferenceExpressionReason(expression); reason != "" {
		reference.Reason = reason
		return reference
	}
	schema := model.ModelPricingUsageSchema(manifest)
	// The first example must be complete; never invent facts or try a later one.
	for field := range schema.Fields {
		if _, exists := example.Facts[field]; !exists {
			reference.Reason = "invalid_example"
			return reference
		}
	}
	facts, err := TaskExpressionUsage(example.Facts, schema)
	if err != nil {
		reference.Reason = "invalid_example"
		return reference
	}
	instant := time.Unix(0, 0).UTC()
	input := billingexpr.RequestInput{Usage: facts, Evaluation: billingexpr.NewEvaluation()}
	input.Evaluation.Now = func() time.Time { return instant }
	charge, err := EvaluatePricingPreview(name, values, true, schema, billing.Usage{InputIncludesCache: true, Source: "catalog_reference", Complete: true, Terminal: "completed"}, input, instant)
	if err != nil {
		reference.Reason = "calculation_failed"
		common.SysError("catalog reference calculation failed")
		return reference
	}
	reference.Status, reference.AmountUSD = "available", charge.TotalUSD
	if charge.Trace != nil {
		reference.MatchedTier = charge.Trace.MatchedTier
	}
	return reference
}

func publicUsageReferences(price model.Pricing) model.Pricing {
	if price.BillingMode == "tiered_expr" && price.BillingPluginKey != "" {
		price.UsageReference = taskUsageReference(price.ModelName, price.BillingPluginKey, price.BillingPluginName, price.PricingValues, price.BillingUsageFacts, price.BillingUsageExamples)
	}
	// GetPricing returns cached records; copy the slice before enriching it.
	price.BillingPluginVariants = append([]model.PricingPluginVariant(nil), price.BillingPluginVariants...)
	for index := range price.BillingPluginVariants {
		variant := &price.BillingPluginVariants[index]
		if variant.BillingMode != "tiered_expr" {
			continue
		}
		values := model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": variant.BillingExpr}
		variant.UsageReference = taskUsageReference(price.ModelName, variant.PluginKey, variant.PluginName, values, variant.BillingUsageFacts, variant.BillingUsageExamples)
	}
	return price
}
