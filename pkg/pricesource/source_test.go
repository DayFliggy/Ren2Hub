package pricesource

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// axonCost 把单个模型的 cost 对象包装成 AxonHub all.json 文档。
func axonCost(cost string) string {
	return `{"providers":{"v":{"models":{"m":{"cost":` + cost + `}}}}}`
}

func TestPriceSourceDraftPreservesUnitsAndZero(t *testing.T) {
	for _, tc := range []struct{ name, source, body, expression string }{
		{"sub2api exact decimal", "sub2api", `{"example":{"input_cost_per_token":0.000001234567890123456789,"output_cost_per_token":0,"cache_read_input_token_cost":0}}`, `tier("base", p * 1.234567890123456789 + c * 0 + cr * 0)`},
		{"axonhub model array", "axonhub", `{"providers":{"vendor":{"models":[{"id":"model","cost":{"input":2,"output":6,"cacheRead":0,"cacheWrite":2.5}}]}}}`, `tier("base", p * 2 + c * 6 + cr * 0 + cc * 2.5)`},
		{"axonhub keyed models", "axonhub", axonCost(`{"input":2,"output":6}`), `tier("base", p * 2 + c * 6)`},
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
	tiers := `{"input":2,"output":6,"cache_read":0.2,"tiers":[{"input":4,"output":12,"cache_read":0.4,"tier":{"type":"context","size":100}},{"input":8,"output":24,"cache_read":0.8,"tier":{"type":"context","size":200}}]}`
	for _, tc := range []struct {
		name, source, document string
		length                 int64
		expected               float64
	}{
		{"catalog first threshold inclusive base", "axonhub", axonCost(tiers), 100, 3.2},
		{"catalog second tier", "axonhub", axonCost(tiers), 201, 12.8},
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
		{"unknown paid dimension", "axonhub", axonCost(`{"input":1,"output":2,"unknown":4}`)},
		{"different reasoning price", "axonhub", axonCost(`{"input":1,"output":2,"reasoning":4}`)},
		{"conflicting tiers", "axonhub", axonCost(`{"input":1,"output":2,"context_over_200k":{"input":3,"output":6},"tiers":[{"input":4,"output":8,"tier":{"type":"context","size":200000}}]}`)},
		{"same prices with conflicting thresholds", "axonhub", axonCost(`{"input":1,"output":2,"context_over_200k":{"input":3,"output":6},"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":100}}]}`)},
		{"nested nonzero context prices", "axonhub", axonCost(`{"input":1,"output":2,"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":100},"context_over_200k":{"input":100,"output":200}}]}`)},
		{"unmapped context selector", "axonhub", axonCost(`{"input":1,"output":2,"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":100,"service_tier":"priority"}}]}`)},
		{"duplicate price fields", "axonhub", axonCost(`{"input":1,"input":0,"output":2}`)},
		{"missing base cache dimension", "axonhub", axonCost(`{"input":1,"output":2,"tiers":[{"input":4,"output":8,"cache_read":1,"tier":{"type":"context","size":200000}}]}`)},
		{"duplicate thresholds", "axonhub", axonCost(`{"input":1,"output":2,"tiers":[{"input":2,"tier":{"type":"context","size":10}},{"input":3,"tier":{"type":"context","size":10}}]}`)},
		{"partial priority", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0.000002}}`},
		{"category only in priority", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_priority":0.000002,"output_cost_per_token_priority":0.000004,"cache_read_input_token_cost_priority":0.0000002}}`},
		{"unknown context policy", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"long_context_input_token_threshold":100,"long_context_unknown_multiplier":2}}`},
		{"range tiers with separate context surcharge", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"long_context_input_token_threshold":100,"long_context_input_cost_multiplier":2,"tiered_pricing":[{"range":[0,200000],"input_cost_per_token":0.000001,"output_cost_per_token":0.000002}]}}`},
		{"mixed image and token", "sub2api", `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"output_cost_per_image":0.04}}`},
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

// Sub2API 认识但本地不计费的字段只警告；其他未知计价字段仍整条拒收。
func TestSub2APIUnbilledFieldsWarnButUnknownChargesReject(t *testing.T) {
	base := `"input_cost_per_token":0.000001,"output_cost_per_token":0.000002`
	models, err := Parse("sub2api", []byte(`{"m":{`+base+`,"cache_storage_cost_per_token_per_hour":0.00000025}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	assert.Equal(t, `tier("base", p * 1 + c * 2)`, models[0].Pricing["billing_setting.billing_expr"])
	assert.Equal(t, []string{"cache_storage_cost_per_token_per_hour is not billed locally; imported prices exclude it"}, models[0].Warnings)

	models, err = Parse("sub2api", []byte(`{"m":{`+base+`,"cache_storage_cost_per_token_per_hour":0}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	assert.Empty(t, models[0].Warnings)

	models, err = Parse("sub2api", []byte(`{"m":{`+base+`,"input_cost_per_video_per_second":0.1}}`))
	require.NoError(t, err)
	assert.Equal(t, "unsupported charge field: input_cost_per_video_per_second", models[0].UnsupportedReason)
	assert.Empty(t, models[0].Pricing)
}

func TestPriceSourceKeepsProviderIdentitiesAndSeparateProducts(t *testing.T) {
	models, err := Parse("axonhub", []byte(`{"providers":{"a":{"models":{"m":{"cost":{"input":3,"output":6}}}},"b":{"models":{"m":{"cost":{"input":1,"output":2}}}}}}`))
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "a", models[0].Provider)
	assert.Equal(t, "b", models[1].Provider)
	assert.NotEqual(t, models[0].Pricing, models[1].Pricing)
	models, err = Parse("sub2api", []byte(`{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"input_cost_per_token_batches":0.0000005}}`))
	require.NoError(t, err)
	require.Empty(t, models[0].UnsupportedReason)
	assert.Contains(t, models[0].Warnings, "batch product prices are not imported")
}

func TestPriceSourceRejectsUnmappedToolFeesUnlessExplicitlyZero(t *testing.T) {
	for _, field := range []string{"search_context_cost_per_query", "google_maps_grounding_cost_per_query"} {
		document := `{"m":{"input_cost_per_token":0.000001,"output_cost_per_token":0.000002,"` + field + `":%s}}`
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
			t.Run(field+"/"+tc.name, func(t *testing.T) {
				models, err := Parse("sub2api", []byte(fmt.Sprintf(document, tc.value)))
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

func TestPriceSourceSelectionAndDocumentValidation(t *testing.T) {
	_, err := Select([]string{"sub2api", "sub2api"})
	assert.ErrorContains(t, err, "duplicate")
	_, err = Select([]string{"http://localhost"})
	assert.ErrorContains(t, err, "unknown")
	// 这三个来源由原生 ratio_sync 预设处理，本地不再接受。
	for _, id := range []string{"basellm", "models_dev", "openrouter"} {
		_, err = Select([]string{id})
		assert.ErrorContains(t, err, "unknown price source", id)
		_, err = Parse(id, []byte(`{}`))
		assert.ErrorContains(t, err, "unknown price source", id)
	}
	for _, source := range Sources() {
		_, err := Parse(source.ID, []byte(`{}`))
		assert.Error(t, err, source.ID)
	}
	_, err = Parse("axonhub", []byte(`{"providers":{"v":{"models":[{"id":"m","cost":{"input":0,"output":0}},{"id":"m","cost":{"input":1,"output":1}}]}}}`))
	assert.ErrorContains(t, err, "duplicate model")
}

func TestPriceSourceRejectsAmbiguousObjectIdentities(t *testing.T) {
	for _, tc := range []struct{ source, body string }{
		{"sub2api", `{"m":{"input_cost_per_token":1,"output_cost_per_token":2},"m":{"input_cost_per_token":0,"output_cost_per_token":0}}`},
		{"axonhub", `{"providers":{"v":{"models":{"m":{"cost":{"input":1,"output":2}},"m":{"cost":{"input":0,"output":0}}}}}}`},
		{"axonhub", `{"providers":{"v":{"models":{}},"v":{"models":{}}}}`},
	} {
		models, err := Parse(tc.source, []byte(tc.body))
		require.Error(t, err, tc.source)
		assert.Empty(t, models)
	}
}

func TestPriceSourceAcceptsEquivalentContextDeclarations(t *testing.T) {
	models, err := Parse("axonhub", []byte(axonCost(`{"input":1,"output":2,"context_over_200k":{"input":3,"output":6},"tiers":[{"input":3,"output":6,"tier":{"type":"context","size":200000}}]}`)))
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
