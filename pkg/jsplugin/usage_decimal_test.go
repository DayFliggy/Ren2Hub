package jsplugin

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestUsageBoundaryAcceptsOnlyExactNumbers(t *testing.T) {
	engine, err := Compile(`export function extractUsageOnComplete(ctx){return {seconds:ctx.value};} export function ordinary(ctx){return ctx.value;}`, Options{})
	require.NoError(t, err)
	for _, tc := range []struct {
		value any
		want  any
	}{
		{1.5, "1.5"},
		{0.3, "0.3"},
		{-2.25, "-2.25"},
		{1e-7, "0.0000001"},
		{float64(5), int64(5)},
		{int64(1<<53 - 1), int64(1<<53 - 1)},
		{"7.500000000000000001", "7.500000000000000001"},
	} {
		value, err := engine.Call(t.Context(), "extractUsageOnComplete", map[string]any{"value": tc.value})
		require.NoError(t, err, "%v", tc.value)
		assert.Equal(t, map[string]any{"seconds": tc.want}, value, "%v", tc.value)
	}
	// Go folds constant expressions exactly, so build the float64 sum at run time.
	tenth, fifth, three := 0.1, 0.2, 3.0
	noisy := tenth + fifth
	for _, value := range []any{math.NaN(), math.Inf(1), int64(1 << 53), float64(1 << 53), noisy, 1 / three} {
		_, err = engine.Call(t.Context(), "extractUsageOnComplete", map[string]any{"value": value})
		require.Error(t, err, "%v", value)
	}
	// Ordinary hooks are outside the billing boundary.
	value, err := engine.Call(t.Context(), "ordinary", map[string]any{"value": noisy})
	require.NoError(t, err)
	assert.Equal(t, noisy, value)
	for body, message := range map[string]string{
		`return {seconds:0.1+0.2};`:          "17 significant digits",
		`return {seconds:9007199254740992};`: "unsafe plugin integer",
		`return {seconds:1/3};`:              "significant digits",
	} {
		engine, err := Compile(`export function extractUsage(){`+body+`}`, Options{})
		require.NoError(t, err)
		_, err = engine.Call(t.Context(), "extractUsage")
		require.ErrorContains(t, err, message, body)
		require.ErrorContains(t, err, "inexact usage", body)
	}
}

func TestUsageHooksSeeSourceDecimalsWithoutHostRounding(t *testing.T) {
	engine, err := Compile(`
export function extractUsage(ctx){return {seconds:utils.decimal.add(ctx.a,ctx.b)};}
export function extractUsageOnComplete(ctx, _result, body){return {kinds:[typeof body.short, typeof body.long, typeof body.huge], seconds:body.short*2};}
`, Options{})
	require.NoError(t, err)
	value, err := engine.Call(t.Context(), "extractUsage", map[string]any{"a": json.Number("0.100000000000000001"), "b": json.Number("0.2")})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"seconds": "0.300000000000000001"}, value)
	// A source number reaches the hook as a number only when the boundary
	// would return that same decimal; other source text stays a string.
	body := map[string]any{"short": json.Number("2.5"), "long": json.Number("3.500000000000000001"), "huge": json.Number("9007199254740993")}
	value, err = engine.Call(t.Context(), "extractUsageOnComplete", nil, nil, body)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"kinds": []any{"number", "string", "string"}, "seconds": int64(5)}, value)
}

func TestUsageDecimalMultiplyPreservesExactProducts(t *testing.T) {
	engine, err := Compile(`
export function extractUsage(){
  return {
    fractional: utils.decimal.multiply("0.2", "3.5"),
    repeating: utils.decimal.multiply("0.1", "0.2"),
  };
}
`, Options{})
	require.NoError(t, err)

	value, err := engine.Call(t.Context(), "extractUsage")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"fractional": "0.7", "repeating": "0.02"}, value)
}

func TestUsageHooksIgnoreUnrelatedSettingsButRejectInexactArithmetic(t *testing.T) {
	engine, err := Compile(`
export function extractUsage(ctx){return {clips:ctx.request.count};}
export function extractUsageOnComplete(ctx){return {seconds:ctx.request.offset*3};}
`, Options{})
	require.NoError(t, err)
	input := map[string]any{"request": map[string]any{"count": int64(2), "offset": 0.1, "unrelated": []any{map[string]any{"temperature": 0.7}}}}
	value, err := engine.Call(t.Context(), "extractUsage", input)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"clips": int64(2)}, value)
	// 0.1*3 is 0.30000000000000004: billing it as 0.3 would round silently.
	_, err = engine.Call(t.Context(), "extractUsageOnComplete", input)
	require.ErrorContains(t, err, "significant digits")
	// A subsequent use of the pooled runtime retains the same boundary.
	_, err = engine.Call(t.Context(), "extractUsageOnComplete", input)
	require.Error(t, err)
}

func TestDynamicCodeIsDisabledInsideHooks(t *testing.T) {
	for name, body := range map[string]string{
		"eval":                 `return eval("1");`,
		"indirect eval":        `return (0, eval)("1");`,
		"Function":             `return Function("return 1")();`,
		"new Function":         `return new Function("return 1")();`,
		"function constructor": `return (function(){}).constructor("return 1")();`,
	} {
		engine, err := Compile(`export function run() { `+body+` }`, Options{Key: "dynamic"})
		require.NoError(t, err, name)
		_, err = engine.Call(t.Context(), "run")
		var hookErr *HookError
		require.ErrorAs(t, err, &hookErr, name)
		assert.ErrorContains(t, err, "EvalError", name)
	}
}

func TestFactoryCompletionFactsUseExactProviderUnits(t *testing.T) {
	for _, tc := range []struct {
		name       string
		task, body map[string]any
		field      string
		want       any
	}{
		{"vidu", nil, map[string]any{"credits": json.Number("3.500000000000000001")}, "credits", "3.500000000000000001"},
		{"kling", nil, map[string]any{"data": map[string]any{"final_unit_deduction": json.Number("3.500000000000000001")}}, "units", "3.500000000000000001"},
		{"sora", nil, map[string]any{"seconds": json.Number("4.000000000000000001")}, "seconds", "4.000000000000000001"},
		{"alibaba", map[string]any{"upstreamModel": "wan3.0-video"}, map[string]any{"usage": map[string]any{"input_video_duration": json.Number("0.1"), "output_video_duration": json.Number("0.2")}}, "seconds", "0.3"},
		{"doubao", map[string]any{"upstreamModel": "doubao-seedance-2-0-260128"}, map[string]any{"status": "succeeded", "usage": map[string]any{"completion_tokens": json.Number("1001")}}, "tokens", int64(1001)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("..", "..", "plugins", "tasks", tc.name, "plugin.js"))
			require.NoError(t, err)
			engine, err := Compile(string(source), Options{})
			require.NoError(t, err)
			result, err := engine.Call(t.Context(), "extractUsageOnComplete", tc.task, nil, tc.body)
			require.NoError(t, err)
			facts, ok := result.(map[string]any)
			require.True(t, ok)
			assert.Equal(t, tc.want, facts[tc.field])
		})
	}
}

func TestFactoryMissingCompletionDoesNotInventFacts(t *testing.T) {
	for _, name := range []string{"vidu", "kling", "google", "vertex-ai", "jimeng"} {
		t.Run(name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("..", "..", "plugins", "tasks", name, "plugin.js"))
			require.NoError(t, err)
			engine, err := Compile(string(source), Options{})
			require.NoError(t, err)
			result, err := engine.Call(t.Context(), "extractUsageOnComplete", nil, nil, map[string]any{})
			require.NoError(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestPluginDecimalBoundsDoNotRoundInvalidIntegers(t *testing.T) {
	engine, err := Compile(`export function extractUsage(ctx){return {count:utils.decimal.bounded(ctx.count,0,128,true)};}`, Options{})
	require.NoError(t, err)
	for _, value := range []any{json.Number("1.000000000000000001"), json.Number("129"), json.Number("-1"), nil, "NaN"} {
		_, err := engine.Call(t.Context(), "extractUsage", map[string]any{"count": value})
		require.Error(t, err)
	}
	result, err := engine.Call(t.Context(), "extractUsage", map[string]any{"count": json.Number("0")})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"count": "0"}, result)
}

func TestStreamJSONPreservesUsageNumbersBeforeJSCanRoundThem(t *testing.T) {
	engine, err := Compile(`export function parseSubmitEvent(event){return utils.json.parseNumbers(event.data);}`, Options{})
	require.NoError(t, err)
	value, err := engine.Call(t.Context(), "parseSubmitEvent", map[string]any{"data": `{"count":1.000000000000000001,"seconds":7.500000000000000001,"tokens":9007199254740993}`})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"count": "1.000000000000000001", "seconds": "7.500000000000000001", "tokens": "9007199254740993"}, value)
}
