package pricesource

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceSourceDraftPreservesUnitsAndZero(t *testing.T) {
	for _, tc := range []struct{ name, source, body, expression string }{
		{"sub2api exact decimal", "sub2api", `{"example":{"input_cost_per_token":0.000001234567890123456789,"output_cost_per_token":0,"cache_read_input_token_cost":0}}`, `tier("base", p * 1.234567890123456789 + c * 0 + cr * 0)`},
		{"axonhub million tokens", "axonhub", `{"providers":{"vendor":{"models":[{"id":"model","cost":{"input":2,"output":6,"cacheRead":0,"cacheWrite":2.5}}]}}}`, `tier("base", p * 2 + c * 6 + cr * 0 + cc * 2.5)`},
		{"models dev million tokens", "models_dev", `{"vendor":{"models":{"model":{"cost":{"input":2,"output":6}}}}}`, `tier("base", p * 2 + c * 6)`},
		{"openrouter string tokens", "openrouter", `{"data":[{"id":"vendor/model","pricing":{"prompt":"0.000002","completion":"0.000006","input_cache_write_1h":"0.000004"}}]}`, `tier("base", p * 2 + c * 6 + cc1h * 4)`},
		{"basellm literal expression", "basellm", `{"success":true,"data":{"billing_mode":{"model":"tiered_expr"},"billing_expr":{"model":"tier(\"custom\", p * 0.123456789 + c * 0)"}}}`, `tier("custom", p * 0.123456789 + c * 0)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, err := Parse(tc.source, []byte(tc.body))
			require.NoError(t, err)
			require.Len(t, models, 1)
			assert.Empty(t, models[0].UnsupportedReason)
			assert.Equal(t, tc.expression, models[0].Pricing["billing_setting.billing_expr"])
			assert.Equal(t, "tiered_expr", models[0].Pricing["billing_setting.billing_mode"])
		})
	}
}

func TestPriceSourceContextBoundariesAndCacheCharges(t *testing.T) {
	for _, tc := range []struct {
		name, source, document string
		length                 int64
		expected               float64
	}{
		{"catalog first threshold inclusive base", "models_dev", `{"v":{"models":{"m":{"cost":{"input":2,"output":6,"cache_read":0.2,"tiers":[{"input":4,"output":12,"cache_read":0.4,"tier":{"type":"context","size":100}},{"input":8,"output":24,"cache_read":0.8,"tier":{"type":"context","size":200}}]}}}}}`, 100, 3.2},
		{"catalog second tier", "models_dev", `{"v":{"models":{"m":{"cost":{"input":2,"output":6,"cache_read":0.2,"tiers":[{"input":4,"output":12,"cache_read":0.4,"tier":{"type":"context","size":100}},{"input":8,"output":24,"cache_read":0.8,"tier":{"type":"context","size":200}}]}}}}}`, 201, 12.8},
		{"openrouter minimum inclusive", "openrouter", `{"data":[{"id":"m","pricing":{"prompt":"0.000002","completion":"0.000006","input_cache_read":"0.0000002","overrides":[{"min_prompt_tokens":100,"prompt":"0.000004","completion":"0.000012","input_cache_read":"0.0000004"}]}}]}`, 100, 6.4},
		{"sub2api scales cache with input", "sub2api", `{"m":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000006,"cache_read_input_token_cost":0.0000002,"long_context_input_token_threshold":100,"long_context_input_cost_multiplier":2,"long_context_output_cost_multiplier":2}}`, 101, 6.4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, err := Parse(tc.source, []byte(tc.document))
			require.NoError(t, err)
			require.Empty(t, models[0].UnsupportedReason)
			expression := models[0].Pricing["billing_setting.billing_expr"].(string)
			// RunExpr returns micro-USD; wallet quota conversion happens later.
			amount, _, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{P: 1, C: 0, CR: 6, Len: float64(tc.length)})
			require.NoError(t, err)
			assert.InDelta(t, tc.expected, amount, 1e-12)
		})
	}
}

func TestPriceSourceRejectsLossyOrUnknownPrices(t *testing.T) {
	for _, tc := range []struct{ name, source, body string }{
		{"missing output is not free", "openrouter", `{"data":[{"id":"m","pricing":{"prompt":"0.000002"}}]}`},
		{"malformed output is not free", "openrouter", `{"data":[{"id":"m","pricing":{"prompt":"0.000002","completion":"bad"}}]}`},
		{"dynamic sentinel", "openrouter", `{"data":[{"id":"m","pricing":{"prompt":"-1","completion":"-1"}}]}`},
		{"nested nonzero overrides", "openrouter", `{"data":[{"id":"m","pricing":{"prompt":"0.000002","completion":"0.000004","overrides":[{"min_prompt_tokens":100,"prompt":"0.000003","overrides":[{"min_prompt_tokens":200,"prompt":"0.0001"}]}]}}]}`},
		{"conditional base price", "openrouter", `{"data":[{"id":"m","pricing":{"prompt":"0.000002","completion":"0.000004","min_prompt_tokens":100}}]}`},
		{"unknown paid dimension", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"unknown":4}}}}}`},
		{"different reasoning price", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"reasoning":4}}}}}`},
		{"conflicting tiers", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"context_over_200k":{"input":3,"output":6},"tiers":[{"input":4,"output":8,"tier":{"type":"context","size":200000}}]}}}}}`},
		{"same prices with conflicting thresholds", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"context_over_200k":{"input":3,"output":6},"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":100}}]}}}}}`},
		{"nested nonzero context prices", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":100},"context_over_200k":{"input":100,"output":200}}]}}}}}`},
		{"unmapped context selector", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":100,"service_tier":"priority"}}]}}}}}`},
		{"duplicate price fields", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"input":0,"output":2}}}}}`},
		{"missing base cache dimension", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":4,"output":8,"cache_read":1,"tier":{"type":"context","size":200000}}]}}}}}`},
		{"partial priority", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0.000002}}`},
		{"category only in priority", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0.000002,"output_cost_per_token_priority":0.000004,"cache_read_input_token_cost_priority":0.0000002}}`},
		{"unknown context policy", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"long_context_input_token_threshold":100,"long_context_unknown_multiplier":2}}`},
		{"range tiers with separate context surcharge", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"long_context_input_token_threshold":100,"long_context_input_cost_multiplier":2,"tiered_pricing":[{"range":[0,200000],"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}]}}`},
		{"mixed image and token", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"output_cost_per_image":0.04}}`},
		{"invalid expression", "basellm", `{"success":true,"data":{"billing_mode":{"m":"tiered_expr"},"billing_expr":{"m":"unknown(42)"}}}`},
		{"duplicate thresholds", "models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2,"tiers":[{"input":2,"tier":{"type":"context","size":10}},{"input":3,"tier":{"type":"context","size":10}}]}}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			models, err := Parse(tc.source, []byte(tc.body))
			require.NoError(t, err)
			require.Len(t, models, 1)
			assert.NotEmpty(t, models[0].UnsupportedReason)
			assert.Empty(t, models[0].Pricing)
		})
	}
}

func TestPriceSourceKeepsProviderIdentitiesAndSeparateProducts(t *testing.T) {
	models, err := Parse("models_dev", []byte(`{"a":{"models":{"m":{"cost":{"input":3,"output":6}}}},"b":{"models":{"m":{"cost":{"input":1,"output":2}}}}}`))
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "a", models[0].Provider)
	assert.Equal(t, "b", models[1].Provider)
	assert.NotEqual(t, models[0].Pricing, models[1].Pricing)
	models, err = Parse("openrouter", []byte(`{"data":[{"id":"vendor/model:batch","pricing":{"prompt":"0.000001","completion":"0.000002","web_search":"0"}}]}`))
	require.NoError(t, err)
	assert.Equal(t, "vendor/model:batch", models[0].ModelName)
	assert.Empty(t, models[0].UnsupportedReason)
	assert.NotEmpty(t, models[0].Warnings)
	models, err = Parse("sub2api", []byte(`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_batches":0.0000005}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	assert.Contains(t, models[0].Warnings, "batch product prices are not imported")
}

func TestPriceSourceRejectsUnmappedToolFeesUnlessExplicitlyZero(t *testing.T) {
	for _, field := range []struct{ source, name, document string }{
		{"openrouter", "web_search", `{"data":[{"id":"m","pricing":{"prompt":"0.000001","completion":"0.000002","web_search":%s}}]}`},
		{"openrouter", "tier_web_search", `{"data":[{"id":"m","pricing":{"prompt":"0.000001","completion":"0.000002","overrides":[{"min_prompt_tokens":100,"web_search":%s}]}}]}`},
		{"sub2api", "search_context_cost_per_query", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"search_context_cost_per_query":%s}}`},
		{"sub2api", "google_maps_grounding_cost_per_query", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"google_maps_grounding_cost_per_query":%s}}`},
	} {
		for _, tc := range []struct {
			name, value string
			allowed     bool
		}{
			{"numeric zero", `0`, true},
			{"decimal string zero", `"0.000"`, true},
			{"scientific zero", `"0e-10"`, true},
			{"nested explicit zeros", `{"low":0,"other":{"high":"0.00","medium":0e-6}}`, true},
			{"nonzero", `0.01`, false},
			{"nonzero string", `"0.01"`, false},
			{"invalid string", `"unknown"`, false},
			{"negative", `-1`, false},
			{"null is missing", `null`, false},
			{"boolean is not a price", `false`, false},
			{"empty object is unknown", `{}`, false},
			{"array is unknown", `[0,0]`, false},
			{"nested nonzero", `{"low":0,"other":{"high":0.01}}`, false},
			{"nested invalid", `{"low":0,"other":{"high":null}}`, false},
			{"nested empty", `{"low":0,"other":{}}`, false},
			{"ambiguous duplicate", `{"low":0.01,"low":0}`, false},
		} {
			t.Run(field.source+"/"+field.name+"/"+tc.name, func(t *testing.T) {
				models, err := Parse(field.source, []byte(fmt.Sprintf(field.document, tc.value)))
				require.NoError(t, err)
				require.Len(t, models, 1)
				if tc.allowed {
					require.Empty(t, models[0].UnsupportedReason)
					assert.NotEmpty(t, models[0].Pricing)
					require.Len(t, models[0].Warnings, 1)
					assert.Contains(t, models[0].Warnings[0], "explicitly zero")
					return
				}
				assert.Contains(t, models[0].UnsupportedReason, "tool fee")
				assert.Empty(t, models[0].Pricing)
			})
		}
	}
}

func TestPriceSourceServiceTierAndImageQuantities(t *testing.T) {
	models, err := Parse("sub2api", []byte(`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0.000003,"output_cost_per_token_priority":0.000006}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	expression := models[0].Pricing["billing_setting.billing_expr"].(string)
	for _, tc := range []struct {
		body     string
		expected float64
	}{{`{}`, 3}, {`{"service_tier":"priority"}`, 9}} {
		amount, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{P: 1, C: 1}, billingexpr.RequestInput{Body: []byte(tc.body)})
		require.NoError(t, err)
		assert.InDelta(t, tc.expected, amount, 1e-12)
	}
	models, err = Parse("sub2api", []byte(`{"image":{"output_cost_per_image":0.04}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	n := 3
	amount, _, err := billingexpr.RunExprWithRequest(models[0].Pricing["billing_setting.billing_expr"].(string), billingexpr.TokenParams{}, billingexpr.RequestInput{ImageCount: &n})
	require.NoError(t, err)
	assert.Equal(t, float64(120000), amount)
}

func TestPriceSourceLegacyDraftAndSourceValidation(t *testing.T) {
	models, err := Parse("basellm", []byte(`{"success":true,"data":{"model_ratio":{"m":1.234567890123456789},"completion_ratio":{"m":0}}}`))
	require.NoError(t, err)
	assert.Equal(t, "1.234567890123456789", models[0].Pricing["ModelRatio"])
	assert.Equal(t, "0", models[0].Pricing["CompletionRatio"])
	_, err = Select([]string{"basellm", "basellm"})
	assert.ErrorContains(t, err, "duplicate")
	_, err = Select([]string{"http://localhost"})
	assert.ErrorContains(t, err, "unknown")
	for _, source := range Sources() {
		_, err := Parse(source.ID, []byte(`{}`))
		assert.Error(t, err, source.ID)
	}
	_, err = Parse("openrouter", []byte(`{"data":[{"id":"m","pricing":{"prompt":"0","completion":"0"}},{"id":"m","pricing":{"prompt":"1","completion":"1"}}]}`))
	assert.ErrorContains(t, err, "duplicate model")
}

func TestLegacyPriceDraftRetainsDecimalsAcrossBrowserJSON(t *testing.T) {
	models, err := Parse("basellm", []byte(`{"success":true,"data":{"model_ratio":{"m":1.234567890123456789e-7},"completion_ratio":{"m":0},"cache_ratio":{"m":"2.5e-3"}}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	encoded, err := json.Marshal(models[0])
	require.NoError(t, err)
	var wire struct {
		Pricing map[string]string `json:"pricing"`
	}
	// Strings survive JSON.parse in the editor, unlike numeric literals.
	require.NoError(t, json.Unmarshal(encoded, &wire))
	assert.Equal(t, "0.0000001234567890123456789", wire.Pricing["ModelRatio"])
	assert.Equal(t, "0", wire.Pricing["CompletionRatio"])
	assert.Equal(t, "0.0025", wire.Pricing["CacheRatio"])
}

func TestPriceSourceRejectsAmbiguousObjectIdentities(t *testing.T) {
	for _, tc := range []struct{ source, body string }{
		{"sub2api", `{"m":{"input_cost_per_token":1,"output_cost_per_token":2},"m":{"input_cost_per_token":0,"output_cost_per_token":0}}`},
		{"models_dev", `{"v":{"models":{"m":{"cost":{"input":1,"output":2}},"m":{"cost":{"input":0,"output":0}}}}}`},
		{"basellm", `{"success":true,"data":{"model_ratio":{"m":1,"m":0}}}`},
	} {
		models, err := Parse(tc.source, []byte(tc.body))
		require.Error(t, err, tc.source)
		assert.Empty(t, models)
	}
}

func TestPriceSourceAcceptsEquivalentContextDeclarations(t *testing.T) {
	models, err := Parse("models_dev", []byte(`{"v":{"models":{"m":{"cost":{"input":1,"output":2,"context_over_200k":{"input":3,"output":6},"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":200000}}]}}}}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	expression := models[0].Pricing["billing_setting.billing_expr"].(string)
	for _, tc := range []struct {
		length int64
		amount float64
	}{{200000, 1}, {200001, 3}} {
		amount, _, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{P: 1, Len: float64(tc.length)})
		require.NoError(t, err)
		assert.Equal(t, tc.amount, amount)
	}
}
