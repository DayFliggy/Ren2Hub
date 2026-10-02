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

func TestTaskPricingCompatibilityPreservesFrozenAndStoredPrices(t *testing.T) {
	modelPricingConfigDB(t)
	const oldSource = `export const meta={apiVersion:1,key:"compat-price",name:"Compatibility",version:"1.0.0",author:{name:"Test"},models:["image"],fetchMode:"per_task",
usageProfiles:[{models:["image"],schema:{images:{type:"number",unit:"count"},old_images:{type:"number",unit:"count"}}}]};
export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};`
	const expression = `u("old_images") * 0.01 + u("images") * 0.02`
	plugin, err := jsplugin.DefaultRegistry.Register(oldSource, jsplugin.Options{})
	require.NoError(t, err)
	pricing := PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"compat-price": expression}}
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "image", ExpectedVersion: ModelPricingVersion(PricingValues{}), Pricing: pricing}}))
	oldRef := TaskPluginRef{Key: plugin.Meta.Key, Version: plugin.Meta.Version, SourceHash: plugin.SourceHash}
	oldPrice, err := ResolveTaskModelBillingPrice(t.Context(), "image", "image", oldRef)
	require.NoError(t, err)
	oldJSON, err := common.Marshal(oldPrice)
	require.NoError(t, err)
	newSource := strings.Replace(oldSource, `version:"1.0.0"`, `version:"1.1.0"`, 1)
	newSource = strings.Replace(newSource, `schema:{images:{type:"number",unit:"count"},old_images:{type:"number",unit:"count"}}`, `schema:{images:{type:"number",unit:"count"}},compatibilitySchema:{old_images:{type:"number",unit:"count"}}`, 1)
	plugin, err = jsplugin.DefaultRegistry.Register(newSource, jsplugin.Options{})
	require.NoError(t, err)
	snapshot, err := GetModelPricingSnapshot([]string{"image"})
	require.NoError(t, err)
	require.Len(t, snapshot.Entries, 1)
	entry := snapshot.Entries[0]
	assert.NotContains(t, entry.UsageSchema, "old_images")
	require.Len(t, entry.PluginVariants, 1)
	assert.NotContains(t, entry.PluginVariants[0].UsageSchema, "old_images")
	assert.True(t, entry.PluginVariants[0].Compatible, "released expressions stay routable")
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "image", ExpectedVersion: entry.Version, Pricing: pricing}}))
	newRef := TaskPluginRef{Key: plugin.Meta.Key, Version: plugin.Meta.Version, SourceHash: plugin.SourceHash}
	price, err := ResolveTaskModelBillingPrice(t.Context(), "image", "image", newRef)
	require.NoError(t, err)
	assert.Equal(t, expression, price.Expression)
	assert.Contains(t, price.UsageSchema.Fields, "images")
	assert.Contains(t, price.UsageSchema.Fields, "old_images")
	assert.Equal(t, "completion_fact", price.UsageSchema.Fields["old_images"].Source)
	after, err := common.Marshal(oldPrice)
	require.NoError(t, err)
	assert.JSONEq(t, string(oldJSON), string(after), "new code must not mutate an in-flight quote")
	_, err = ResolveTaskModelBillingPrice(t.Context(), "image", "image", oldRef)
	require.ErrorContains(t, err, "changed before quoting")

	changed := PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"compat-price": `u("old_images") * 0.03`}}
	err = UpdateModelPricing([]ModelPricingChange{{ModelName: "image", ExpectedVersion: entry.Version, Pricing: changed}})
	require.Error(t, err, "new prices cannot advertise compatibility-only capabilities")
	changed = PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"compat-price": `u("images") * 0.03`}}
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "image", ExpectedVersion: entry.Version, Pricing: changed}}))
}
