package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/plugins"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViduPricePublicationRejectsUnreservableCredits(t *testing.T) {
	registry := taskPriceTestRegistry(t)
	source, err := plugins.Source("vidu")
	require.NoError(t, err)
	_, err = registry.RegisterFactory(source, jsplugin.Options{})
	require.NoError(t, err)
	for _, name := range []string{"viduq2", "viduq1", "vidu2.0", "vidu1.5"} {
		t.Run(name, func(t *testing.T) {
			current, err := ResolveTaskPluginPriceSchema(name, "vidu")
			require.NoError(t, err)
			for _, expression := range []string{`v2:u("credits") * 0.4`, `v2:true ? 1 : u("credits")`} {
				_, err := ValidateTaskPluginPricePublication(billing.PriceVersion{
					Model: name, Mode: billing.ModeExpression, Expression: expression, UsageSchema: current.UsageSchema,
				}, "vidu")
				require.ErrorContains(t, err, "credits has no reviewed reservation estimate")
			}
			_, err = ValidateTaskPluginPricePublication(billing.PriceVersion{
				Model: name, Mode: billing.ModeUnitPrice, UsageSchema: current.UsageSchema,
				Prices: []billing.UnitPrice{{Quantity: "credits", Unit: "per_credit", Price: "0.4"}},
			}, "vidu")
			require.ErrorContains(t, err, "credits has no reviewed reservation estimate")
		})
	}
}

func TestViduPublicationGuardPreservesAcceptedCreditQuotes(t *testing.T) {
	registry := taskPriceTestRegistry(t)
	source, err := plugins.Source("vidu")
	require.NoError(t, err)
	_, err = registry.RegisterFactory(source, jsplugin.Options{})
	require.NoError(t, err)
	current, err := ResolveTaskPluginPriceSchema("viduq2", "vidu")
	require.NoError(t, err)
	credits := current.UsageSchema.Fields["credits"]
	credits.Required = true
	current.UsageSchema.Fields["credits"] = credits
	current.UsageSchema.Hash = ""
	old, err := model.PublishBillingPrice(context.Background(), billing.PriceVersion{
		Model: "viduq2", Mode: billing.ModeExpression, Expression: `v2:u("credits") * 0.4`, UsageSchema: current.UsageSchema,
	}, 0, 1)
	require.NoError(t, err)
	t.Cleanup(func() { model.DB.Where("model = ?", "viduq2").Delete(&model.BillingActivePrice{}) })
	quote, err := billing.NewQuote(*old, "1", nil, time.Unix(1000, 0))
	require.NoError(t, err)
	_, err = ValidateTaskPluginPricePublication(*old, "vidu")
	require.ErrorContains(t, err, "credits has no reviewed reservation estimate")
	stored, err := model.GetActiveBillingPrice(context.Background(), "viduq2")
	require.NoError(t, err)
	assert.Equal(t, old, stored)
	result, err := billing.Calculate(quote, billing.Usage{Complete: true, ActualFacts: map[string]any{"credits": 10}})
	require.NoError(t, err)
	assert.Equal(t, "4", result.TotalUSD)
	_, err = billing.Calculate(quote, billing.Usage{Complete: true, EstimatedFacts: map[string]any{"credits": 10}})
	require.ErrorContains(t, err, "missing", "new publication validation must not turn estimates into completion facts")
}

func TestPluginPerCallPricePublishesAndSettlesWithoutUsageDimensions(t *testing.T) {
	registry := taskPriceTestRegistry(t)
	source, err := plugins.Source("vidu")
	require.NoError(t, err)
	_, err = registry.RegisterFactory(source, jsplugin.Options{})
	require.NoError(t, err)
	price, err := ValidateTaskPluginPricePublication(billing.PriceVersion{
		Model: "viduq2", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.25"}},
	}, "vidu")
	require.NoError(t, err)
	quote, err := billing.NewQuote(price, "1", nil, time.Unix(1000, 0))
	require.NoError(t, err)
	estimate := &billing.Usage{Estimated: true, Quantities: map[string]string{"calls": "1"}}
	precharge, err := billing.Calculate(quote, *estimate)
	require.NoError(t, err)
	assert.Equal(t, "0.25", precharge.TotalUSD)
	for _, facts := range []map[string]any{nil, {"credits": 10}} {
		task := &model.Task{Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{
			PluginRef:      &model.TaskPluginRef{Key: "vidu", Version: "fixed", SourceHash: "fixed"},
			BillingContext: &model.TaskBillingContext{Quote: quote, Usage: estimate},
		}}
		quota, _, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{UsageFacts: facts})
		require.NoError(t, err)
		assert.EqualValues(t, 125000, quota)
		assert.Empty(t, task.PrivateData.BillingContext.Usage.ActualFacts)
		assert.Equal(t, map[string]string{"calls": "1"}, estimate.Quantities)
	}
	for _, price := range []billing.PriceVersion{
		{Model: "viduq2", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "seconds", Unit: "per_second", Price: "0.25"}}},
		{Model: "viduq2", Mode: billing.ModeExpression, Expression: `v1:fixed(0.25)`},
		{Model: "viduq2", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.25"}, {Quantity: "tool:search", Unit: "per_call", Price: "1"}}},
	} {
		_, err := ValidateTaskPluginPricePublication(price, "vidu")
		require.Error(t, err, "schema-free publication is restricted to the single fixed call dimension")
	}
	_, err = ValidateTaskPluginPricePublication(billing.PriceVersion{
		Model: "viduq2", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "-1"}},
	}, "vidu")
	require.Error(t, err)
}
