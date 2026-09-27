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

func TestUsageHooksPreserveDecimalTextAndRejectFloatingArithmetic(t *testing.T) {
	engine, err := Compile(`export function extractUsage(ctx){return {seconds:utils.decimal.add(ctx.a,ctx.b)};} export function extractUsageOnComplete(ctx){return {seconds:ctx.value};} export function ordinary(ctx){return ctx.value;}`, Options{})
	require.NoError(t, err)
	value, err := engine.Call(t.Context(), "extractUsage", map[string]any{"a": json.Number("0.100000000000000001"), "b": json.Number("0.2")})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"seconds": "0.300000000000000001"}, value)
	for _, value := range []any{0.3, math.NaN(), math.Inf(1), int64(1 << 53), float64(1 << 53)} {
		_, err = engine.Call(t.Context(), "extractUsageOnComplete", map[string]any{"value": value})
		require.Error(t, err)
	}
	value, err = engine.Call(t.Context(), "ordinary", map[string]any{"value": 0.3})
	require.NoError(t, err)
	assert.Equal(t, 0.3, value)
	for _, body := range []string{`return {seconds:0.1+0.2};`, `return {seconds:9007199254740992};`, `return {seconds:utils.decimal.string(0.3)};`} {
		engine, err := Compile(`export function extractUsage(){`+body+`}`, Options{})
		require.NoError(t, err)
		_, err = engine.Call(t.Context(), "extractUsage")
		require.Error(t, err)
	}
}

func TestFactoryCompletionFactsUseExactProviderUnits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		task, body  map[string]any
		field, want string
	}{
		{"vidu", nil, map[string]any{"credits": json.Number("3.500000000000000001")}, "credits", "3.500000000000000001"},
		{"kling", nil, map[string]any{"data": map[string]any{"final_unit_deduction": json.Number("3.500000000000000001")}}, "units", "3.500000000000000001"},
		{"sora", nil, map[string]any{"seconds": json.Number("4.000000000000000001")}, "seconds", "4.000000000000000001"},
		{"alibaba", map[string]any{"upstreamModel": "wan3.0-video"}, map[string]any{"usage": map[string]any{"input_video_duration": json.Number("0.1"), "output_video_duration": json.Number("0.2")}}, "seconds", "0.3"},
		{"doubao", map[string]any{"upstreamModel": "doubao-seedance-2-0-260128"}, map[string]any{"status": "succeeded", "usage": map[string]any{"completion_tokens": json.Number("1001")}}, "tokens", "1001"},
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
