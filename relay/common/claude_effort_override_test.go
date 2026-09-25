package common

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeEffectiveEffortParameterOverrides(t *testing.T) {
	const input = `{"output_config":{"effort":"medium"},"messages":[{"role":"system","content":[],"output_config":{"effort":"high"}},{"role":"user","content":"hi"}]}`
	for _, tc := range []struct {
		name, mode, path, effort string
		value                    any
	}{
		{name: "change_message", mode: "set", path: "messages.0.output_config.effort", value: "LOW", effort: "low"},
		{name: "delete_message_uses_top_level", mode: "delete", path: "messages.0.output_config", effort: "medium"},
		{name: "delete_top_level_keeps_message", mode: "delete", path: "output_config", effort: "high"},
		{name: "delete_user_leaves_pending", mode: "delete", path: "messages.1", effort: "medium"},
		{name: "unrelated_parameter", mode: "set", path: "max_tokens", value: 128, effort: "high"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &RelayInfo{RelayFormat: types.RelayFormatClaude, ReasoningEffort: "stale", ChannelMeta: &ChannelMeta{
				ParamOverride: map[string]any{"operations": []any{map[string]any{"mode": tc.mode, "path": tc.path, "value": tc.value}}},
			}}
			_, err := ApplyParamOverrideWithRelayInfo([]byte(input), info)
			require.NoError(t, err)
			assert.Equal(t, tc.effort, info.ReasoningEffort)
		})
	}
	info := &RelayInfo{RelayFormat: types.RelayFormatClaude, ReasoningEffort: "high", ChannelMeta: &ChannelMeta{
		ParamOverride: map[string]any{"operations": []any{map[string]any{"mode": "delete", "path": "messages.0.output_config"}}},
	}}
	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"}]}`), info)
	require.NoError(t, err)
	assert.Empty(t, info.ReasoningEffort)
}

func TestClaudeEffectiveEffortOverrideRejectsInvalidResult(t *testing.T) {
	info := &RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &ChannelMeta{
		ParamOverride: map[string]any{"operations": []any{map[string]any{"mode": "set", "path": "output_config.effort", "value": false}}},
	}}
	_, err := ApplyParamOverrideWithRelayInfo([]byte(`{}`), info)
	require.ErrorContains(t, err, "output_config.effort")
	assert.True(t, reasoning.IsClientError(err))
}
