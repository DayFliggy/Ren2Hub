package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
