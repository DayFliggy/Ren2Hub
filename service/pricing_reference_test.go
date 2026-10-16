package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogUsageReferenceUsesFirstExampleAndExistingCalculator(t *testing.T) {
	old := common.CryptoSecret
	common.CryptoSecret = "reference-preview-test-key"
	t.Cleanup(func() { common.CryptoSecret = old })
	schema := map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}, "quality": {Enum: []string{"low", "high"}}}
	examples := []jsplugin.UsageExample{{Label: "first · 2.5s", Facts: map[string]any{"seconds": "2.5", "quality": "high"}}, {Label: "second", Facts: map[string]any{"seconds": "1", "quality": "low"}}}
	for _, tc := range []struct{ name, expression, amount, reason string }{
		{"pure usage", `u("quality") == "high" ? tier("high", u("seconds") * 0.4) : tier("low", u("seconds") * 0.2)`, "1", ""},
		{"zero", `tier("free", u("seconds") * 0)`, "0", ""},
		{"no clock even in unselected branch", `true ? u("seconds") : hour("UTC")`, "", "dynamic_context"},
		{"no request probe", `u("seconds") * (param("mode") == nil ? 1 : 2)`, "", "dynamic_context"},
		{"no header", `u("seconds") * (header("x") == "" ? 1 : 2)`, "", "dynamic_context"},
		{"no dynamic usage access", `u("seconds") + u(u("quality"))`, "", "dynamic_context"},
		{"invalid result", `u("seconds") / 0`, "", "calculation_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := taskUsageReference("task", "alpha", "Alpha", model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": tc.expression}, schema, examples)
			require.NotNil(t, result)
			assert.Equal(t, tc.amount, result.AmountUSD)
			assert.Equal(t, tc.reason, result.Reason)
			assert.Equal(t, "first · 2.5s", result.Label)
			assert.Equal(t, "2.5", result.Facts["seconds"])
			if tc.reason == "" {
				assert.Equal(t, "available", result.Status)
			} else {
				assert.Equal(t, "unavailable", result.Status)
			}
		})
	}
	values := model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("seconds") * 0.4`}
	assert.Equal(t, "missing_example", taskUsageReference("task", "alpha", "Alpha", values, schema, nil).Reason)
	broken := jsplugin.CloneUsageExamples(examples)
	broken[0].Facts["seconds"] = "3601"
	assert.Equal(t, "invalid_example", taskUsageReference("task", "alpha", "Alpha", values, schema, broken).Reason, "must not substitute the valid second example")
	delete(broken[0].Facts, "seconds")
	assert.Equal(t, "invalid_example", taskUsageReference("task", "alpha", "Alpha", values, schema, broken).Reason)
	assert.Equal(t, "2.5", examples[0].Facts["seconds"], "preview cannot mutate the plugin generation")
}

func TestPublicUsageReferencesKeepVariantsAndCachedRecordsSeparate(t *testing.T) {
	old := common.CryptoSecret
	common.CryptoSecret = "reference-preview-test-key"
	t.Cleanup(func() { common.CryptoSecret = old })
	schema := map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}}
	price := model.Pricing{ModelName: "task", BillingPluginVariants: []model.PricingPluginVariant{
		{PluginKey: "alpha", PluginName: "Alpha", BillingMode: "tiered_expr", BillingExpr: `u("seconds") * 0.4`, BillingUsageSchema: schema, BillingUsageFacts: schema},
		{PluginKey: "beta", PluginName: "Beta", BillingMode: "tiered_expr", BillingExpr: `u("seconds") * 0.1`, BillingUsageSchema: schema, BillingUsageFacts: schema, BillingUsageExamples: []jsplugin.UsageExample{{Label: "second provider", Facts: map[string]any{"seconds": "2"}}}},
	}}
	result := publicUsageReferences(price)
	assert.Equal(t, "missing_example", result.BillingPluginVariants[0].UsageReference.Reason)
	assert.Equal(t, "0.2", result.BillingPluginVariants[1].UsageReference.AmountUSD)
	assert.Nil(t, price.BillingPluginVariants[0].UsageReference)
	assert.Nil(t, price.BillingPluginVariants[1].UsageReference)
}
