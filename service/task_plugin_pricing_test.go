package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func taskPriceTestRegistry(t *testing.T) *jsplugin.Registry {
	t.Helper()
	previous := jsplugin.DefaultRegistry
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { jsplugin.DefaultRegistry = previous })
	return jsplugin.DefaultRegistry
}

func registerPriceTestPlugin(t *testing.T, registry *jsplugin.Registry, key, version, enum string, channelType int) {
	t.Helper()
	source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:"Pricing fixture",version:%q,author:{name:"Test"},channelTypes:[%d],models:["task-price-fixture"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second"},resolution:{enum:%s}}};export function buildSubmitRequest(){return {url:"https://example.com"}};export function parseSubmitResponse(){return {taskId:"job"}};export function buildQueryRequest(){return {url:"https://example.com"}};export function parseTaskResult(){return {status:"SUCCESS"}};`, key, version, channelType, enum)
	_, err := registry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
}

func taskPriceFixture() billing.PriceVersion {
	return billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "test retail price"}, Model: "task-price-fixture", Mode: billing.ModeExpression,
		Expression: `v2:u("seconds") * (u("resolution") == "1080p" ? 0.4 : 0.2)`,
		UsageSchema: &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
			"seconds":    {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact, Required: true, Maximum: "60"},
			"resolution": {Type: "string", Enum: []string{"720p", "1080p"}, Source: billingexpr.UsageRequestFixed, Required: true},
		}}}
}

func TestTaskPluginPricePublicationRejectsNarrowedEnumsAndPreservesFrozenQuote(t *testing.T) {
	registry := taskPriceTestRegistry(t)
	registerPriceTestPlugin(t, registry, "price-fixture", "1.0.0", `["720p","1080p"]`, 24)
	ctx := context.Background()
	defer model.DB.Where("model = ?", "task-price-fixture").Delete(&model.BillingActivePrice{})
	price, err := ValidateTaskPluginPricePublication(taskPriceFixture(), "price-fixture")
	require.NoError(t, err)
	published, err := model.PublishBillingPrice(ctx, price, 0, 1)
	require.NoError(t, err)
	quote, err := billing.NewQuoteWithFacts(*published, "1", nil, time.Unix(1000, 0), map[string]any{"resolution": "1080p"})
	require.NoError(t, err)
	quoteHash, schemaHash := quote.Hash, quote.PriceVersion.UsageSchema.Hash
	registerPriceTestPlugin(t, registry, "price-fixture", "2.0.0", `["720p"]`, 24)
	_, err = ValidateTaskPluginPricePublication(*published, "price-fixture")
	require.ErrorContains(t, err, "1080p")
	current, err := model.GetActiveBillingPrice(ctx, price.Model)
	require.NoError(t, err)
	assert.Equal(t, published, current, "reading an old price does not rewrite the historical schema")
	charge, err := billing.Calculate(quote, billing.Usage{Complete: true, ActualFacts: map[string]any{"seconds": 10}})
	require.NoError(t, err)
	assert.Equal(t, "4", charge.TotalUSD)
	assert.Equal(t, quoteHash, quote.Hash)
	assert.Equal(t, schemaHash, quote.PriceVersion.UsageSchema.Hash)

	for _, expression := range []string{
		`v2:u("seconds") * (u("resolution") == "1080p" ? 0.4 : 0.2)`,
		`v2:true ? u("seconds") : ((u("resolution") == "720p" || u("resolution") == "1080p") ? 1 : 0)`,
	} {
		draft := taskPriceFixture()
		draft.UsageSchema.Fields["resolution"] = billingexpr.UsageField{Type: "string", Source: billingexpr.UsageRequestFixed, Required: true, Enum: []string{"720p"}}
		draft.Expression = expression
		_, err := ValidateTaskPluginPricePublication(draft, "price-fixture")
		require.ErrorContains(t, err, "unsupported enum value")
	}
	draft := taskPriceFixture()
	draft.UsageSchema.Fields["resolution"] = billingexpr.UsageField{Type: "string", Source: billingexpr.UsageRequestFixed, Required: true, Enum: []string{"720p"}}
	draft.Expression = `v2:u("seconds") * (u("resolution") == "720p" ? 0.2 : 0.1)`
	_, err = ValidateTaskPluginPricePublication(draft, "price-fixture")
	require.NoError(t, err)
}

func TestTaskPluginPriceSchemaRequiresExplicitProviderAndReviewedSources(t *testing.T) {
	registry := taskPriceTestRegistry(t)
	registerPriceTestPlugin(t, registry, "price-alpha", "1.0.0", `["720p","1080p"]`, 24)
	registerPriceTestPlugin(t, registry, "price-beta", "1.0.0", `["720p"]`, 41)
	current, err := ResolveTaskPluginPriceSchema("task-price-fixture", "")
	require.NoError(t, err)
	require.Len(t, current.Plugins, 2)
	assert.Empty(t, current.PluginKey)
	assert.Nil(t, current.UsageSchema)
	assert.True(t, current.ReviewRequired)
	_, err = ValidateTaskPluginPricePublication(taskPriceFixture(), "")
	require.ErrorContains(t, err, "plugin_key is required")
	current, err = ResolveTaskPluginPriceSchema("task-price-fixture", "price-beta")
	require.NoError(t, err)
	assert.Equal(t, []string{"720p"}, current.ManifestSchema["resolution"].Enum)
	assert.Nil(t, current.UsageSchema, "custom metadata cannot invent fact source")
	_, err = ResolveTaskPluginPriceSchema("task-price-fixture", "missing")
	require.Error(t, err)
	current, err = ResolveTaskPluginPriceSchema("ordinary-model", "")
	require.NoError(t, err)
	assert.Empty(t, current.Plugins)
	assert.False(t, current.ReviewRequired)
	assert.Nil(t, current.UsageSchema)

	source, err := plugins.Source("alibaba")
	require.NoError(t, err)
	_, err = registry.RegisterFactory(source, jsplugin.Options{})
	require.NoError(t, err)
	current, err = ResolveTaskPluginPriceSchema("wan2.6-t2i", "alibaba")
	require.NoError(t, err)
	require.NotNil(t, current.UsageSchema)
	assert.False(t, current.ReviewRequired)
	field := current.UsageSchema.Fields["image_count"]
	assert.Equal(t, billingexpr.UsageCompletionFact, field.Source)
	price := billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "test retail price"}, Model: "wan2.6-t2i", Mode: billing.ModeExpression, Expression: `v2:u("image_count") * 0.2`, UsageSchema: current.UsageSchema}
	_, err = ValidateTaskPluginPricePublication(price, "alibaba")
	require.NoError(t, err)
	for _, mutation := range []func(*billingexpr.UsageField){
		func(f *billingexpr.UsageField) { f.Source = billingexpr.UsageRequestFixed },
		func(f *billingexpr.UsageField) { f.Maximum = "1000" },
		func(f *billingexpr.UsageField) { f.Integer = false },
	} {
		changed := field
		mutation(&changed)
		price.UsageSchema = &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{"image_count": changed}}
		_, err := ValidateTaskPluginPricePublication(price, "alibaba")
		require.ErrorContains(t, err, "reviewed fact source or bounds")
	}
}
