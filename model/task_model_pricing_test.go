package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTaskPricingValuesPreservesAliasAndPluginPrecedence(t *testing.T) {
	schema := map[string]jsplugin.UsageFieldSchema{"count": {Type: "number", Unit: "count"}}
	aliasExpression := PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("count") * 0.03`}
	declaredExpression := PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("count") * 0.04`}
	aliasOverride := PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"plugin": `u("count") * 0.01`}}
	declaredOverride := PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"plugin": `u("count") * 0.02`}}
	for _, test := range []struct {
		name         string
		candidates   []PricingValues
		expression   string
		price        any
		configured   bool
		incompatible bool
	}{
		{name: "alias plugin override wins", candidates: []PricingValues{aliasOverride, declaredOverride}, expression: `u("count") * 0.01`, configured: true},
		{name: "mapped plugin override wins over alias expression", candidates: []PricingValues{aliasExpression, declaredOverride}, expression: `u("count") * 0.02`, configured: true},
		{name: "alias expression wins over mapped expression", candidates: []PricingValues{aliasExpression, declaredExpression}, expression: `u("count") * 0.03`, configured: true},
		{name: "mapped expression fallback", candidates: []PricingValues{{}, declaredExpression}, expression: `u("count") * 0.04`, configured: true},
		{name: "only the executing plugin selects an override", candidates: []PricingValues{{billing_setting.PluginBillingExprOption: map[string]any{"other": `u("missing")`}}, declaredExpression}, expression: `u("count") * 0.04`, configured: true},
		{name: "explicit alias zero wins over mapped fixed price", candidates: []PricingValues{{"ModelPrice": "0"}, {"ModelPrice": "1"}}, price: "0", configured: true},
		{name: "mapped fixed price is not inherited", candidates: []PricingValues{{}, {"ModelPrice": "1"}}},
		{name: "incompatible override cannot fall back", candidates: []PricingValues{{"ModelPrice": "0", billing_setting.PluginBillingExprOption: map[string]any{"plugin": `u("missing")`}}, declaredExpression}, incompatible: true},
		{name: "empty override cannot fall back", candidates: []PricingValues{{billing_setting.PluginBillingExprOption: map[string]any{"plugin": ""}}, declaredExpression}, incompatible: true},
		{name: "incompatible alias expression cannot fall back", candidates: []PricingValues{{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("missing")`}, declaredExpression}, incompatible: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, err := common.Marshal(test.candidates)
			require.NoError(t, err)
			effective, taskUsage, err := ResolveTaskPricingValues("plugin", schema, test.candidates...)
			if test.incompatible {
				require.ErrorContains(t, err, "not configured for the executing task plugin")
				assert.Nil(t, effective)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.configured, ModelPriceConfigured(effective))
				assert.Equal(t, test.expression != "", taskUsage)
				assert.Equal(t, test.price, effective["ModelPrice"])
				if taskUsage {
					assert.Equal(t, test.expression, effective["billing_setting.billing_expr"])
				}
			}
			after, err := common.Marshal(test.candidates)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after), "resolving one plugin must not change another plugin's snapshot")
		})
	}
}

func TestTaskModelPricingUsesMappedProfileAndExecutingPlugin(t *testing.T) {
	modelPricingConfigDB(t)
	source := `export const meta={apiVersion:1,key:"profile-price",name:"Profile",version:"1.0.0",author:{name:"Test"},models:["video","image"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second"}},usageProfiles:[{models:["image"],schema:{count:{type:"number",unit:"count"}}}]};
export function buildSubmitRequest(){return {}};
export function parseSubmitResponse(){return {}};
export function buildQueryRequest(){return {}};
export function parseTaskResult(){return {}};`
	plugin, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "image", ExpectedVersion: ModelPricingVersion(PricingValues{}), Pricing: PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"profile-price": `u("count") * 0.02`}}}}))
	ref := TaskPluginRef{Key: plugin.Meta.Key, Version: plugin.Meta.Version, SourceHash: plugin.SourceHash}
	price, err := ResolveTaskModelBillingPrice(t.Context(), "public-alias", "image", ref)
	require.NoError(t, err)
	assert.True(t, price.TaskUsage)
	assert.Equal(t, "public-alias", price.Model)
	assert.Equal(t, `u("count") * 0.02`, price.Expression)
	assert.Contains(t, price.UsageSchema.Fields, "count")
	assert.NotContains(t, price.UsageSchema.Fields, "seconds")
	ref.SourceHash = strings.Repeat("0", 64)
	_, err = ResolveTaskModelBillingPrice(t.Context(), "public-alias", "image", ref)
	require.ErrorContains(t, err, "changed before quoting")
}
