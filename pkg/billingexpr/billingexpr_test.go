package billingexpr_test

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNewAPIExpressionModes(t *testing.T) {
	for _, tc := range []struct {
		name, expression string
		tokens           billingexpr.TokenParams
		input            billingexpr.RequestInput
		want             float64
		tier             string
	}{
		{"context tier", `len <= 200000 ? tier("base", p*3+c*15+cr*0.3) : tier("long",p*6+c*22.5+cr*0.6)`, billingexpr.TokenParams{P: 100000, C: 1000, Len: 200001, CR: 100001}, billingexpr.RequestInput{}, 682500.6, "long"},
		{"fixed image", `tier("image",fixed(0.04))*image_count`, billingexpr.TokenParams{}, billingexpr.RequestInput{}, 40000, "image"},
		{"dynamic paths", `param(param("key")) * (header(param("head")) == "yes" ? 3 : 1)`, billingexpr.TokenParams{}, billingexpr.RequestInput{Body: []byte(`{"key":"nested.rate","nested":{"rate":2.5},"head":"X-Mode"}`), Headers: map[string]string{"x-mode": "yes"}}, 7.5, ""},
		{"full expression grammar", `sum(map([1,2,3], # * 2)) + (5 % 2)`, billingexpr.TokenParams{}, billingexpr.RequestInput{}, 13, ""},
		{"task usage", `tier("task",u("seconds")*0.4)`, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: map[string]any{"seconds": float64(10)}}, 4, "task"},
		{"missing and explicit zero", `param("missing") == nil && param("zero") == 0 && param("flag") == false ? 2 : 9`, billingexpr.TokenParams{}, billingexpr.RequestInput{Body: []byte(`{"zero":0,"flag":false}`)}, 2, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, trace, err := billingexpr.RunExprWithRequest(tc.expression, tc.tokens, tc.input)
			require.NoError(t, err)
			assert.InDelta(t, tc.want, value, 1e-9)
			assert.Equal(t, tc.tier, trace.MatchedTier)
		})
	}
}

func TestEvaluationRecordsEachClockReadAndReplaysAfterRestart(t *testing.T) {
	expression := `hour(param("zone"))*100 + minute("UTC") + now(timezone("UTC")).Hour()`
	evaluation := billingexpr.NewEvaluation()
	read := 0
	evaluation.Now = func() time.Time { read++; return time.Date(2026, 9, 30, 10+read, read, 0, 0, time.UTC) }
	input := billingexpr.RequestInput{Body: []byte(`{"zone":"UTC"}`), Evaluation: evaluation}
	value, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.NoError(t, err)
	assert.Equal(t, float64(1115), value)
	encoded, err := common.Marshal(evaluation.Calls)
	require.NoError(t, err)
	var calls []billingexpr.EnvironmentCall
	require.NoError(t, common.Unmarshal(encoded, &calls))
	billingexpr.InvalidateCache()
	input.Evaluation = billingexpr.ReplayEvaluation(calls)
	replayed, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.NoError(t, err)
	assert.Equal(t, value, replayed)
	input.Evaluation = billingexpr.ReplayEvaluation(calls[:len(calls)-1])
	_, _, err = billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.Error(t, err)
}

func TestMapIterationAndTimeBuiltinsUseRecordedEnvironment(t *testing.T) {
	expression := `values(param("rates"))[0] + (date("2026-09-30").Day() == 30 ? 1 : 0)`
	evaluation := billingexpr.NewEvaluation()
	input := billingexpr.RequestInput{Body: []byte(`{"rates":{"a":2,"b":7,"c":12}}`), Evaluation: evaluation}
	value, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.NoError(t, err)
	input.Evaluation = billingexpr.ReplayEvaluation(evaluation.Calls)
	replayed, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.NoError(t, err)
	assert.Equal(t, value, replayed)
	input.Body = []byte(`{"rates":{"a":2,"b":8,"c":12}}`)
	input.Evaluation = billingexpr.ReplayEvaluation(evaluation.Calls)
	_, _, err = billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.Error(t, err)
}

func TestExpressionHashAndInvalidMoneyAreRejected(t *testing.T) {
	_, _, err := billingexpr.RunExprByHash("p", billingexpr.ExprHashString("c"), billingexpr.TokenParams{})
	require.ErrorContains(t, err, "hash")
	for _, expression := range []string{`1.0/0.0`, `-1.0`, `0.0/0.0`} {
		_, _, err := billingexpr.RunExpr(expression, billingexpr.TokenParams{})
		require.Error(t, err, expression)
	}
}

func TestRequestRuleTracePreservesNewAPIConditions(t *testing.T) {
	value, trace, err := billingexpr.RunExprWithRequest(`p * (param("service_tier") == "fast" ? 0.8 : 1) * (header("anthropic-beta") == "fast-mode" ? 2 : 1)`, billingexpr.TokenParams{P: 100}, billingexpr.RequestInput{Body: []byte(`{"service_tier":"fast"}`)})
	require.NoError(t, err)
	assert.Equal(t, float64(80), value)
	require.Len(t, trace.RequestRules, 2)
	assert.Equal(t, "0.8", trace.RequestRules[0].Multiplier)
	assert.True(t, trace.RequestRules[0].Matched)
	assert.False(t, trace.RequestRules[1].Matched)
}

func TestTimeObjectReplaysDSTArithmeticFromItsCapturedZone(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	evaluation := billingexpr.NewEvaluation()
	evaluation.Now = func() time.Time { return time.Date(2026, 3, 7, 12, 0, 0, 0, location) }
	expression := `now(timezone("America/New_York")).AddDate(0,0,1).Sub(now(timezone("America/New_York"))).Hours()`
	value, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, billingexpr.RequestInput{Evaluation: evaluation})
	require.NoError(t, err)
	assert.Equal(t, float64(23), value)
	data, err := common.Marshal(evaluation.Calls)
	require.NoError(t, err)
	var saved []billingexpr.EnvironmentCall
	require.NoError(t, common.Unmarshal(data, &saved))
	t.Setenv("ZONEINFO", t.TempDir())
	t.Setenv("TZ", "Pacific/Honolulu")
	replayed, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, billingexpr.RequestInput{Evaluation: billingexpr.ReplayEvaluation(saved)})
	require.NoError(t, err)
	assert.Equal(t, value, replayed)
}

func TestClonePreservesExactQuoteAttributesAndExplicitEmptyProjection(t *testing.T) {
	input := billingexpr.RequestInput{Attributes: map[string]any{"temperature": json.Number("0.700000000000000001")}, Usage: map[string]any{"seconds": float64(2.5)}, Body: []byte(`{"temperature":0.7}`)}
	clone, err := billingexpr.CloneRequestInput(input)
	require.NoError(t, err)
	assert.Equal(t, "0.700000000000000001", clone.Attributes["temperature"])
	assert.Equal(t, float64(2.5), clone.Usage["seconds"])
	assert.Equal(t, input.Body, clone.Body)
	input.Attributes = map[string]any{}
	clone, err = billingexpr.CloneRequestInput(input)
	require.NoError(t, err)
	assert.NotNil(t, clone.Attributes)
	assert.Empty(t, clone.Attributes)
	input.Attributes = nil
	clone, err = billingexpr.CloneRequestInput(input)
	require.NoError(t, err)
	assert.Nil(t, clone.Attributes)
}
