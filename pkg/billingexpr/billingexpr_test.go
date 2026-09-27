package billingexpr_test

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestTaskAttributeDependenciesUseFrozenUsageSchema(t *testing.T) {
	schema, err := billingexpr.FreezeUsageSchema(&billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
		"seconds": {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact},
	}})
	require.NoError(t, err)
	used := billingexpr.UsedAttributes(`v2: attr('service_tier') == "priority" ? u("seconds") * 2 : u("seconds")`, schema)
	assert.True(t, used["service_tier"])
}

func TestDecimalExpressionPricing(t *testing.T) {
	const claude = `p <= 200000 ? tier("standard", p * 1.5 + c * 7.5) : tier("long_context", p * 3 + c * 11.25)`
	const glm = `(p < 32000 && c < 200 ? tier("short",p*2+c*8) : p < 32000 ? tier("output",p*3+c*14) : tier("large",p*4+c*16))/1000000`
	const context = `len <= 200000 ? tier("standard", p*3+c*15+cr*0.3) : tier("long",p*6+c*22.5+cr*0.6)`
	for _, tc := range []struct {
		name, expression string
		tokens           billingexpr.TokenParams
		want, tier       string
	}{
		{"standard", claude, billingexpr.TokenParams{P: 100000, C: 5000}, "187500", "standard"},
		{"long", claude, billingexpr.TokenParams{P: 300000, C: 10000}, "1012500", "long_context"},
		{"boundary", claude, billingexpr.TokenParams{P: 200000, C: 1000}, "307500", "standard"},
		{"zero", claude, billingexpr.TokenParams{}, "0", "standard"},
		{"glm short", glm, billingexpr.TokenParams{P: 15000, C: 100}, "0.0308", "short"},
		{"glm output", glm, billingexpr.TokenParams{P: 15000, C: 500}, "0.052", "output"},
		{"glm large", glm, billingexpr.TokenParams{P: 50000, C: 100}, "0.2016", "large"},
		{"cache split", `p*1.5+c*7.5+cr*0.15+cc*2+cc1h*3`, billingexpr.TokenParams{P: 100000, C: 5000, CR: 10000, CC: 5000, CC1h: 2000}, "205000", ""},
		{"unpriced cache", claude, billingexpr.TokenParams{P: 100000, C: 5000, CR: 99999, CC: 88888}, "187500", "standard"},
		{"context boundary", context, billingexpr.TokenParams{P: 100000, C: 1000, Len: 200000, CR: 100000}, "345000", "standard"},
		{"context plus one", context, billingexpr.TokenParams{P: 100000, C: 1000, Len: 200001, CR: 100001}, "682500.6", "long"},
		{"image", `p*2+c*10+img*5+img_cr*0.1+img_o*20`, billingexpr.TokenParams{P: 1000, C: 500, Img: 200, ImgCR: 10, ImgO: 5}, "8101", ""},
		{"audio", `p*2+c*10+ai*50+ai_cr*5+ao*100`, billingexpr.TokenParams{P: 1000, C: 500, AI: 100, AICR: 2, AO: 50}, "17010", ""},
		{"reasoning", `c*2+reasoning*3`, billingexpr.TokenParams{C: 3, Reasoning: 7}, "27", ""},
		{"math helpers", `max(p,c)*0.5+min(p,c)*0.1+abs(-0.2)+floor(1.9)+ceil(0.1)`, billingexpr.TokenParams{P: 300, C: 500}, "282.2", ""},
		{"exact decimals", `p*0.7+c*0.9`, billingexpr.TokenParams{P: 3, C: 1}, "3", ""},
		{"literal precision", `0.123456789012345678901234567890`, billingexpr.TokenParams{}, "0.12345678901234567890123456789", ""},
		{"integer intermediate", `9223372036854775807 * 9223372036854775807`, billingexpr.TokenParams{}, "85070591730234615847396907784232501249", ""},
		{"division precision", `1 / 3`, billingexpr.TokenParams{}, "0.333333333333333333333333333333333333", ""},
		{"precedence", `1+2*3 == 7 && !(2 > 3) ? 5/2 : 0`, billingexpr.TokenParams{}, "2.5", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, trace, err := billingexpr.RunExpr(tc.expression, tc.tokens)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got.String())
			assert.Equal(t, tc.tier, trace.MatchedTier)
		})
	}
}

func TestRestrictedPlanRejectsEscapeSyntaxInEveryBranch(t *testing.T) {
	for _, expression := range []string{`now()`, `false ? now() : 1`, `$env.p`, `$env["cr"]`, `p + $env.cr`, `let f = fixed; f(1)`, `fixed`, `(fixed)(1)`, `attr`, `param("x")`, `header("authorization")`, `attr("messages")`, `attr("quality" + "x")`, `attr("quality").foo`, `attr("quality")[0]`, `map([1], #)`, `sum([1,2])`, `_trace(0,true,1)`, `5 % 2`, `1 ** 3`, `v3:1`, `hour("")`, `hour("Local")`, `hour(attr("quality"))`, `1e999999`} {
		t.Run(expression, func(t *testing.T) { _, err := billingexpr.CompileFromCache(expression); require.Error(t, err) })
	}
	for _, expression := range []string{`1/0`, `0/0`, `-1`, `"1"`, `true`, `1 + true`} {
		_, _, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{})
		require.Error(t, err, expression)
	}
	_, _, err := billingexpr.RunExpr(`p`, billingexpr.TokenParams{P: -1})
	require.Error(t, err)
}

func TestDependenciesRetainZeroAndInactiveBranches(t *testing.T) {
	vars := billingexpr.UsedVars(`false ? image_count + cr*0 + ai_cr : p`)
	assert.True(t, vars["image_count"])
	assert.True(t, vars["cr"])
	assert.True(t, vars["ai_cr"])
	vars["cr"] = false
	assert.True(t, billingexpr.UsedVars(`false ? image_count + cr*0 + ai_cr : p`)["cr"])
	_, _, err := billingexpr.RunExpr(`false ? image_count : p`, billingexpr.TokenParams{P: 1})
	require.ErrorContains(t, err, "image_count")
}

func TestRequestRuleTraceAndMissingAttributes(t *testing.T) {
	expression := `tier("base",p*2)*(attr("service_tier")=="fast"?2:1)*(has(attr("quality"),"fast-mode")?2.5:1)`
	value, trace, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{P: 10}, billingexpr.RequestInput{Attributes: map[string]any{"service_tier": "fast"}})
	require.NoError(t, err)
	assert.Equal(t, "40", value.String())
	assert.Equal(t, "base", trace.MatchedTier)
	require.Len(t, trace.RequestRules, 2)
	assert.True(t, trace.RequestRules[0].Matched)
	assert.False(t, trace.RequestRules[1].Matched)
	assert.Equal(t, "2.5", trace.RequestRules[1].Multiplier)
	value, trace, err = billingexpr.RunExprWithRequest(`attr("quality")==nil ? 2 : 1.5`, billingexpr.TokenParams{}, billingexpr.RequestInput{})
	require.NoError(t, err)
	assert.Equal(t, "2", value.String())
	assert.Empty(t, trace.RequestRules)
}

func TestFrozenCalendarIsRequiredAndIndependentOfClock(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 17, 0, 0, time.UTC)
	expression := `hour("Asia/Shanghai")+minute("UTC")+weekday("UTC")+month("UTC")+day("UTC")`
	calendars, err := billingexpr.FreezeCalendars(expression, nil, at)
	require.NoError(t, err)
	input := billingexpr.RequestInput{FrozenAt: at, Calendars: calendars}
	for range 2 {
		billingexpr.InvalidateCache()
		got, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
		require.NoError(t, err)
		assert.Equal(t, "31", got.String())
	}
	_, _, err = billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, billingexpr.RequestInput{FrozenAt: at})
	require.ErrorContains(t, err, "not frozen")
	altered := calendars["UTC"]
	altered.Hour++
	calendars["UTC"] = altered
	_, _, err = billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.ErrorContains(t, err, "calendar")
}

func TestUsagePlanSchemaAndExactFacts(t *testing.T) {
	schema, err := billingexpr.FreezeUsageSchema(&billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{"seconds": {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact}}})
	require.NoError(t, err)
	expression := `v2: false ? u("seconds")*0 : u("seconds") * 0.7`
	fields, err := billingexpr.UsedUsageFields(expression, schema)
	require.NoError(t, err)
	assert.True(t, fields["seconds"])
	value, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, billingexpr.RequestInput{UsageSchema: schema, Usage: map[string]any{"seconds": "0.3"}})
	require.NoError(t, err)
	assert.Equal(t, "0.21", value.String())
	for _, expression := range []string{`v2:$env.u("seconds")`, `v2:u`, `v2:let f=u; f("seconds")`, `v2:u("sec"+"onds")`, `v2:false ? u("missing") : 0`, `v2:p`, `v2:fixed(1)`} {
		_, err := billingexpr.CompileWithUsageSchema(expression, schema)
		require.Error(t, err, expression)
	}
	_, _, err = billingexpr.RunExprWithRequest(`v2:false ? u("seconds") : 0`, billingexpr.TokenParams{}, billingexpr.RequestInput{UsageSchema: schema})
	require.ErrorContains(t, err, "missing")
}

func TestFrozenExpressionHashAndCache(t *testing.T) {
	expression := `p*0.7+c*0.9`
	a, _, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{P: 3, C: 1})
	require.NoError(t, err)
	billingexpr.InvalidateCache()
	b, _, err := billingexpr.RunExprByHash(expression, billingexpr.ExprHashString(expression), billingexpr.TokenParams{P: 3, C: 1})
	require.NoError(t, err)
	assert.True(t, a.Equal(b))
	_, _, err = billingexpr.RunExprByHash(expression, billingexpr.ExprHashString(`p`), billingexpr.TokenParams{})
	require.ErrorContains(t, err, "hash")
}

func TestNumericAttributesAndFactsRejectFloatTransport(t *testing.T) {
	for _, value := range []any{"0.700000000000000001", json.Number("0.700000000000000001"), decimal.RequireFromString("0.700000000000000001")} {
		got, _, err := billingexpr.RunExprWithRequest(`attr_num("temperature")*3`, billingexpr.TokenParams{}, billingexpr.RequestInput{Attributes: map[string]any{"temperature": value}})
		require.NoError(t, err)
		assert.Equal(t, "2.100000000000000003", got.String())
	}
	_, _, err := billingexpr.RunExprWithRequest(`attr_num("temperature")`, billingexpr.TokenParams{}, billingexpr.RequestInput{Attributes: map[string]any{"temperature": float64(0.7)}})
	require.Error(t, err)
	_, err = billingexpr.NormalizeUsageValue(billingexpr.UsageField{Type: "number", Unit: "second"}, float64(0.3))
	require.Error(t, err)
	_, err = billingexpr.FreezeCalendars(`hour("Invalid/Zone")`, nil, time.Now())
	require.Error(t, err)
}
