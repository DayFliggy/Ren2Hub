package jsplugin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// moejs shares one deeply frozen set of builtins between every runtime in the
// process, so one plugin cannot patch a prototype that another plugin, or a
// later call on a pooled runtime, would then run.
func TestPluginsCannotPatchSharedBuiltins(t *testing.T) {
	engine, err := Compile(`export function extractUsage(ctx) {
		if (ctx.mode === "prototype") Array.prototype.includes = function () { return true; };
		if (ctx.mode === "json") JSON.stringify = function () { return "{}"; };
		if (ctx.mode === "math") Math.max = function () { return 0; };
		return {patched: [].includes(1)};
	}`, Options{})
	require.NoError(t, err)
	for _, mode := range []string{"prototype", "json", "math"} {
		_, err := engine.Call(t.Context(), "extractUsage", map[string]any{"mode": mode})
		var hookErr *HookError
		require.ErrorAs(t, err, &hookErr, mode)
		assert.Contains(t, err.Error(), "TypeError", mode)
	}
	other, err := Compile(`export function extractUsage() { return {patched: [].includes(1)}; }`, Options{})
	require.NoError(t, err)
	result, err := other.Call(t.Context(), "extractUsage")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"patched": false}, result)
}

func TestUnboundedRecursionIsARangeError(t *testing.T) {
	engine, err := Compile(`function down(n) { return down(n + 1) + 1; }
	export function extractUsage() { return {depth: down(0)}; }`, Options{})
	require.NoError(t, err)
	_, err = engine.Call(t.Context(), "extractUsage")
	var hookErr *HookError
	require.ErrorAs(t, err, &hookErr)
	assert.Contains(t, err.Error(), "Maximum call stack size exceeded")
}
