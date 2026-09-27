package billingexpr_test

import (
	"math"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Claude-style: fixed tiers, input > 200K changes both input & output price
// ---------------------------------------------------------------------------

const claudeExpr = `p <= 200000 ? tier("standard", p * 1.5 + c * 7.5) : tier("long_context", p * 3.0 + c * 11.25)`

func TestClaude_StandardTier(t *testing.T) {
	cost, trace, err := billingexpr.RunExpr(claudeExpr, billingexpr.TokenParams{P: 100000, C: 5000})
	if err != nil {
		t.Fatal(err)
	}
	want := 100000*1.5 + 5000*7.5
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "standard")
	}
}

func TestClaude_LongContextTier(t *testing.T) {
	cost, trace, err := billingexpr.RunExpr(claudeExpr, billingexpr.TokenParams{P: 300000, C: 10000})
	if err != nil {
		t.Fatal(err)
	}
	want := 300000*3.0 + 10000*11.25
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "long_context" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "long_context")
	}
}

func TestClaude_BoundaryExact(t *testing.T) {
	cost, trace, err := billingexpr.RunExpr(claudeExpr, billingexpr.TokenParams{P: 200000, C: 1000})
	if err != nil {
		t.Fatal(err)
	}
	want := 200000*1.5 + 1000*7.5
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "standard")
	}
}

// ---------------------------------------------------------------------------
// GLM-style: multi-condition tiers with both input and output dimensions
// ---------------------------------------------------------------------------

const glmExpr = `
(
	p < 32000 && c < 200 ? tier("tier1_short", (p)*2 + c*8) :
	p < 32000 && c >= 200 ? tier("tier2_long_output", (p)*3 + c*14) :
	tier("tier3_long_input", (p)*4 + c*16)
) / 1000000
`

func TestGLM_Tier1(t *testing.T) {
	cost, trace, err := billingexpr.RunExpr(glmExpr, billingexpr.TokenParams{P: 15000, C: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := (15000.0*2 + 100.0*8) / 1000000
	if math.Abs(cost-want) > 1e-10 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "tier1_short" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "tier1_short")
	}
}

func TestGLM_Tier2(t *testing.T) {
	cost, trace, err := billingexpr.RunExpr(glmExpr, billingexpr.TokenParams{P: 15000, C: 500})
	if err != nil {
		t.Fatal(err)
	}
	want := (15000.0*3 + 500.0*14) / 1000000
	if math.Abs(cost-want) > 1e-10 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "tier2_long_output" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "tier2_long_output")
	}
}

func TestGLM_Tier3(t *testing.T) {
	cost, trace, err := billingexpr.RunExpr(glmExpr, billingexpr.TokenParams{P: 50000, C: 100})
	if err != nil {
		t.Fatal(err)
	}
	want := (50000.0*4 + 100.0*16) / 1000000
	if math.Abs(cost-want) > 1e-10 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "tier3_long_input" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "tier3_long_input")
	}
}

// ---------------------------------------------------------------------------
// Simple flat-rate (no tier() call)
// ---------------------------------------------------------------------------

func TestSimpleExpr_NoTier(t *testing.T) {
	cost, trace, err := billingexpr.RunExpr("p * 0.5 + c * 1.0", billingexpr.TokenParams{P: 1000, C: 500})
	if err != nil {
		t.Fatal(err)
	}
	want := 1000*0.5 + 500*1.0
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "" {
		t.Errorf("tier should be empty, got %q", trace.MatchedTier)
	}
}

// ---------------------------------------------------------------------------
// Math helper functions
// ---------------------------------------------------------------------------

func TestMathHelpers(t *testing.T) {
	cost, _, err := billingexpr.RunExpr("max(p, c) * 0.5 + min(p, c) * 0.1", billingexpr.TokenParams{P: 300, C: 500})
	if err != nil {
		t.Fatal(err)
	}
	want := 500*0.5 + 300*0.1
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestRequestProbeHelpers(t *testing.T) {
	cost, _, err := billingexpr.RunExprWithRequest(
		`p * 0.5 + c * 1.0 * (attr("service_tier") == "fast" ? 2 : 1)`,
		billingexpr.TokenParams{P: 1000, C: 500},
		billingexpr.RequestInput{
			Attributes: map[string]interface{}{"service_tier": "fast"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := 1000*0.5 + 500*1.0*2
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestHeaderProbeHelper(t *testing.T) {
	cost, _, err := billingexpr.RunExprWithRequest(
		`p * 0.5 + c * 1.0 * (has(attr("quality"), "fast-mode") ? 2 : 1)`,
		billingexpr.TokenParams{P: 1000, C: 500},
		billingexpr.RequestInput{
			Attributes: map[string]interface{}{"quality": "fast-mode-2026-02-01"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := 1000*0.5 + 500*1.0*2
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestBillingProbesRejectArbitraryInputs(t *testing.T) {
	for _, expression := range []string{`param("messages.#")`, `header("authorization")`, `attr("messages")`, `attr("stream_options.fast_mode")`, `attr("quality" + "x")`} {
		_, err := billingexpr.CompileFromCache(expression)
		require.Error(t, err, expression)
	}
}

func TestBillingAttributeMissingReturnsNil(t *testing.T) {
	value, _, err := billingexpr.RunExprWithRequest(`attr("quality") == nil ? 2 : 1`, billingexpr.TokenParams{}, billingexpr.RequestInput{})
	require.NoError(t, err)
	assert.Equal(t, float64(2), value)
}

func TestRequestProbeMultipleRulesTraceAllFactors(t *testing.T) {
	exprStr := `(tier("base", p * 2)) * (attr("service_tier") == "fast" ? 2 : 1) * (has(attr("quality"), "fast-mode-2026-02-01") ? 2.5 : 1)`
	cost, trace, err := billingexpr.RunExprWithRequest(
		exprStr,
		billingexpr.TokenParams{P: 10},
		billingexpr.RequestInput{
			Attributes: map[string]interface{}{"service_tier": "fast", "quality": "fast-mode-2026-02-01"},
		},
	)

	require.NoError(t, err)
	assert.InDelta(t, 100, cost, 1e-6)
	assert.Equal(t, "base", trace.MatchedTier)
	assert.Equal(t, []billingexpr.RequestRuleTrace{
		{Cond: `attr("service_tier") == "fast"`, Multiplier: "2", Matched: true},
		{Cond: `has(attr("quality"), "fast-mode-2026-02-01")`, Multiplier: "2.5", Matched: true},
	}, trace.RequestRules)
}

func TestRequestProbeTraceIncludesUnmatchedFactors(t *testing.T) {
	exprStr := `(tier("base", p * 2)) * (attr("service_tier") == "fast" ? 2 : 1) * (has(attr("quality"), "fast-mode") ? 2.5 : 1)`
	cost, trace, err := billingexpr.RunExprWithRequest(
		exprStr,
		billingexpr.TokenParams{P: 10},
		billingexpr.RequestInput{Attributes: map[string]interface{}{"service_tier": "fast"}},
	)

	require.NoError(t, err)
	assert.InDelta(t, 40, cost, 1e-6)
	assert.Equal(t, []billingexpr.RequestRuleTrace{
		{Cond: `attr("service_tier") == "fast"`, Multiplier: "2", Matched: true},
		{Cond: `has(attr("quality"), "fast-mode")`, Multiplier: "2.5", Matched: false},
	}, trace.RequestRules)
}

func TestRequestProbeTracePreservesIntegerConditionalType(t *testing.T) {
	cost, trace, err := billingexpr.RunExprWithRequest(
		`5 % (attr("service_tier") == "fast" ? 2 : 1)`,
		billingexpr.TokenParams{},
		billingexpr.RequestInput{Attributes: map[string]interface{}{"service_tier": "fast"}},
	)

	require.NoError(t, err)
	assert.Equal(t, float64(1), cost)
	assert.Equal(t, []billingexpr.RequestRuleTrace{
		{Cond: `attr("service_tier") == "fast"`, Multiplier: "2", Matched: true},
	}, trace.RequestRules)
}

func TestRequestProbeNonUnitFallbackIsNotTraced(t *testing.T) {
	cost, trace, err := billingexpr.RunExprWithRequest(
		`10 * (attr("service_tier") == "fast" ? 2 : 1.5)`,
		billingexpr.TokenParams{},
		billingexpr.RequestInput{Attributes: map[string]interface{}{"service_tier": "standard"}},
	)

	require.NoError(t, err)
	assert.InDelta(t, 15, cost, 1e-6)
	assert.Empty(t, trace.RequestRules)
}

func TestRequestProbeInternalTraceFunctionIsReserved(t *testing.T) {
	_, err := billingexpr.CompileFromCache(`_trace(0, true, 5.0)`)

	require.ErrorContains(t, err, `identifier "_trace" is reserved for internal use`)
}

func TestCeilFloor(t *testing.T) {
	cost, _, err := billingexpr.RunExpr("ceil(p / 1000) * 0.5", billingexpr.TokenParams{P: 1500})
	if err != nil {
		t.Fatal(err)
	}
	want := math.Ceil(1500.0/1000) * 0.5
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

// ---------------------------------------------------------------------------
// Zero tokens
// ---------------------------------------------------------------------------

func TestZeroTokens(t *testing.T) {
	cost, _, err := billingexpr.RunExpr(claudeExpr, billingexpr.TokenParams{})
	if err != nil {
		t.Fatal(err)
	}
	if cost != 0 {
		t.Errorf("cost should be 0 for zero tokens, got %f", cost)
	}
}

// ---------------------------------------------------------------------------
// Rounding
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Compile errors
// ---------------------------------------------------------------------------

func TestCompileError(t *testing.T) {
	_, _, err := billingexpr.RunExpr("invalid +-+ syntax", billingexpr.TokenParams{})
	if err == nil {
		t.Error("expected compile error")
	}
}

// ---------------------------------------------------------------------------
// Compile Cache
// ---------------------------------------------------------------------------

func TestCompileCache_SameResult(t *testing.T) {
	r1, _, err := billingexpr.RunExpr("p * 0.5", billingexpr.TokenParams{P: 100})
	if err != nil {
		t.Fatal(err)
	}
	r2, _, err := billingexpr.RunExpr("p * 0.5", billingexpr.TokenParams{P: 100})
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 {
		t.Errorf("cached and uncached results differ: %f != %f", r1, r2)
	}
}

func TestInvalidateCache(t *testing.T) {
	billingexpr.InvalidateCache()
	r1, _, _ := billingexpr.RunExpr("p * 0.5", billingexpr.TokenParams{P: 100})
	billingexpr.InvalidateCache()
	r2, _, _ := billingexpr.RunExpr("p * 0.5", billingexpr.TokenParams{P: 100})
	if r1 != r2 {
		t.Errorf("post-invalidate results differ: %f != %f", r1, r2)
	}
}

// ---------------------------------------------------------------------------
// Hash
// ---------------------------------------------------------------------------

func TestExprHashString_Deterministic(t *testing.T) {
	h1 := billingexpr.ExprHashString("p * 0.5")
	h2 := billingexpr.ExprHashString("p * 0.5")
	if h1 != h2 {
		t.Error("hash should be deterministic")
	}
	h3 := billingexpr.ExprHashString("p * 0.6")
	if h1 == h3 {
		t.Error("different expressions should have different hashes")
	}
}

// ---------------------------------------------------------------------------
// Cache variables: present
// ---------------------------------------------------------------------------

const claudeWithCacheExpr = `p <= 200000 ? tier("standard", p * 1.5 + c * 7.5 + cr * 0.15 + cc * 1.875) : tier("long_context", p * 3.0 + c * 11.25 + cr * 0.3 + cc * 3.75)`

func TestCachePresent_StandardTier(t *testing.T) {
	params := billingexpr.TokenParams{P: 100000, C: 5000, CR: 50000, CC: 10000}
	cost, trace, err := billingexpr.RunExpr(claudeWithCacheExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 100000*1.5 + 5000*7.5 + 50000*0.15 + 10000*1.875
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "standard")
	}
}

func TestCachePresent_LongContextTier(t *testing.T) {
	params := billingexpr.TokenParams{P: 300000, C: 10000, CR: 100000, CC: 20000}
	cost, trace, err := billingexpr.RunExpr(claudeWithCacheExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 300000*3.0 + 10000*11.25 + 100000*0.3 + 20000*3.75
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "long_context" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "long_context")
	}
}

// ---------------------------------------------------------------------------
// Cache variables: absent (all zero) — same expression still works
// ---------------------------------------------------------------------------

func TestCacheAbsent_ZeroCacheTokens(t *testing.T) {
	params := billingexpr.TokenParams{P: 100000, C: 5000}
	cost, trace, err := billingexpr.RunExpr(claudeWithCacheExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 100000*1.5 + 5000*7.5
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f (cache terms should be 0)", cost, want)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "standard")
	}
}

// ---------------------------------------------------------------------------
// Mixed cache fields: cc and cc1h non-zero
// ---------------------------------------------------------------------------

const claudeCacheSplitExpr = `tier("default", p * 1.5 + c * 7.5 + cr * 0.15 + cc * 2.0 + cc1h * 3.0)`

func TestMixedCacheFields(t *testing.T) {
	params := billingexpr.TokenParams{P: 100000, C: 5000, CR: 10000, CC: 5000, CC1h: 2000}
	cost, _, err := billingexpr.RunExpr(claudeCacheSplitExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 100000*1.5 + 5000*7.5 + 10000*0.15 + 5000*2.0 + 2000*3.0
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}

func TestMixedCacheFields_AllCacheZero(t *testing.T) {
	params := billingexpr.TokenParams{P: 100000, C: 5000}
	cost, _, err := billingexpr.RunExpr(claudeCacheSplitExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 100000*1.5 + 5000*7.5
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f (all cache zero)", cost, want)
	}
}

// ---------------------------------------------------------------------------
// Backward compatibility: p+c only expressions still work with TokenParams
// ---------------------------------------------------------------------------

func TestBackwardCompat_OldExprWithTokenParams(t *testing.T) {
	params := billingexpr.TokenParams{P: 100000, C: 5000, CR: 99999, CC: 88888}
	cost, trace, err := billingexpr.RunExpr(claudeExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 100000*1.5 + 5000*7.5
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f (old expr ignores cache fields)", cost, want)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want %q", trace.MatchedTier, "standard")
	}
}

// ---------------------------------------------------------------------------
// Time function tests
// ---------------------------------------------------------------------------

func TestBillingTimeIsFrozenAndTimezoneValidated(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 17, 0, 0, time.UTC)
	input := billingexpr.RequestInput{FrozenAt: at}
	expression := `hour("Asia/Shanghai") + minute("UTC") + weekday("UTC") + month("UTC") + day("UTC")`
	value, _, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
	require.NoError(t, err)
	assert.Equal(t, float64(31), value)
	_, _, err = billingexpr.RunExpr(expression, billingexpr.TokenParams{})
	require.ErrorContains(t, err, "not frozen")
	for _, expression := range []string{`hour("")`, `hour("Invalid/Zone")`, `hour("Local")`} {
		_, err := billingexpr.CompileFromCache(expression)
		require.ErrorContains(t, err, "timezone")
	}
	value, _, err = billingexpr.RunExprWithRequest(`p * (hour("UTC") < 6 ? 0.5 : 1)`, billingexpr.TokenParams{P: 100}, input)
	require.NoError(t, err)
	assert.Equal(t, float64(50), value)
}

// ---------------------------------------------------------------------------
// Image and audio token tests
// ---------------------------------------------------------------------------

func TestImageTokenVariable(t *testing.T) {
	exprStr := `tier("base", p * 2 + c * 10 + img * 5)`
	cost, _, err := billingexpr.RunExpr(exprStr, billingexpr.TokenParams{P: 1000, C: 500, Img: 200})
	if err != nil {
		t.Fatal(err)
	}
	// 1000*2 + 500*10 + 200*5 = 2000 + 5000 + 1000 = 8000
	if math.Abs(cost-8000) > 1e-6 {
		t.Errorf("cost = %f, want 8000", cost)
	}
}

func TestAudioTokenVariables(t *testing.T) {
	exprStr := `tier("base", p * 2 + c * 10 + ai * 50 + ao * 100)`
	cost, _, err := billingexpr.RunExpr(exprStr, billingexpr.TokenParams{P: 1000, C: 500, AI: 100, AO: 50})
	if err != nil {
		t.Fatal(err)
	}
	// 1000*2 + 500*10 + 100*50 + 50*100 = 2000 + 5000 + 5000 + 5000 = 17000
	if math.Abs(cost-17000) > 1e-6 {
		t.Errorf("cost = %f, want 17000", cost)
	}
}

func TestImageAudioVariables(t *testing.T) {
	exprStr := `tier("base", p * 1 + img * 3 + ai * 5 + ao * 10)`
	cost, _, err := billingexpr.RunExpr(exprStr, billingexpr.TokenParams{P: 100, Img: 50, AI: 20, AO: 10})
	if err != nil {
		t.Fatal(err)
	}
	// 100*1 + 50*3 + 20*5 + 10*10 = 100 + 150 + 100 + 100 = 450
	if math.Abs(cost-450) > 1e-6 {
		t.Errorf("cost = %f, want 450", cost)
	}
}

func TestImageAudioZero(t *testing.T) {
	exprStr := `tier("base", p * 2 + img * 5 + ai * 50 + ao * 100)`
	cost, _, err := billingexpr.RunExpr(exprStr, billingexpr.TokenParams{P: 1000})
	if err != nil {
		t.Fatal(err)
	}
	// img, ai, ao default to 0
	if math.Abs(cost-2000) > 1e-6 {
		t.Errorf("cost = %f, want 2000", cost)
	}
}

// ---------------------------------------------------------------------------
// len variable tests — tier conditions based on context length
// ---------------------------------------------------------------------------

const lenTieredExpr = `len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6)`

func TestLen_StandardTier(t *testing.T) {
	params := billingexpr.TokenParams{P: 80000, C: 5000, Len: 100000, CR: 20000}
	cost, trace, err := billingexpr.RunExpr(lenTieredExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 80000*3 + 5000*15 + 20000*0.3
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want standard", trace.MatchedTier)
	}
}

func TestLen_LongContextTier(t *testing.T) {
	// p is low (cache subtracted), but len is high (full context)
	params := billingexpr.TokenParams{P: 50000, C: 5000, Len: 300000, CR: 250000}
	cost, trace, err := billingexpr.RunExpr(lenTieredExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	want := 50000*6 + 5000*22.5 + 250000*0.6
	if math.Abs(cost-want) > 1e-6 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
	if trace.MatchedTier != "long_context" {
		t.Errorf("tier = %q, want long_context (len=300000 > 200000)", trace.MatchedTier)
	}
}

func TestLen_BoundaryExact(t *testing.T) {
	params := billingexpr.TokenParams{P: 100000, C: 1000, Len: 200000, CR: 100000}
	_, trace, err := billingexpr.RunExpr(lenTieredExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want standard (len=200000 <= 200000)", trace.MatchedTier)
	}
}

func TestLen_BoundaryPlusOne(t *testing.T) {
	params := billingexpr.TokenParams{P: 100000, C: 1000, Len: 200001, CR: 100001}
	_, trace, err := billingexpr.RunExpr(lenTieredExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	if trace.MatchedTier != "long_context" {
		t.Errorf("tier = %q, want long_context (len=200001 > 200000)", trace.MatchedTier)
	}
}

func TestLen_ZeroDefaultsToZero(t *testing.T) {
	// len defaults to 0 when not set
	params := billingexpr.TokenParams{P: 1000, C: 500}
	_, trace, err := billingexpr.RunExpr(lenTieredExpr, params)
	if err != nil {
		t.Fatal(err)
	}
	if trace.MatchedTier != "standard" {
		t.Errorf("tier = %q, want standard (len=0 <= 200000)", trace.MatchedTier)
	}
}

// ---------------------------------------------------------------------------
// Benchmarks: compile vs cached execution
// ---------------------------------------------------------------------------

const benchComplexExpr = `len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6 + img * 3 + img_o * 30 + ai * 10 + ao * 40) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12 + img * 6 + img_o * 60 + ai * 20 + ao * 80)`

func BenchmarkExprCompile(b *testing.B) {
	for i := 0; i < b.N; i++ {
		billingexpr.InvalidateCache()
		billingexpr.CompileFromCache(benchComplexExpr)
	}
}

func BenchmarkExprRunCached(b *testing.B) {
	billingexpr.CompileFromCache(benchComplexExpr)
	params := billingexpr.TokenParams{P: 150000, C: 10000, Len: 188000, CR: 30000, CC: 5000, Img: 2000, AI: 1000, AO: 500}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		billingexpr.RunExpr(benchComplexExpr, params)
	}
}
