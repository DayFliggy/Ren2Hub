package dto

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeResolveEffectiveEffort(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		effort  string
		present bool
	}{
		{name: "absent", body: `{}`},
		{name: "top_level", body: `{"output_config":{"effort":"medium"}}`, effort: "medium", present: true},
		{name: "message_only", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"}]}`, effort: "high", present: true},
		{name: "message_overrides_top_level", body: `{"output_config":{"effort":"medium"},"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"}]}`, effort: "high", present: true},
		{name: "persists_across_turns", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"},{"role":"assistant"},{"role":"user"}]}`, effort: "high", present: true},
		{name: "switches_at_later_user", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"},{"role":"system","output_config":{"effort":"low"}},{"role":"user"}]}`, effort: "low", present: true},
		{name: "last_pending_wins", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"system","output_config":{"effort":"low"}},{"role":"user"}]}`, effort: "low", present: true},
		{name: "tail_does_not_override_top_level", body: `{"output_config":{"effort":"medium"},"messages":[{"role":"user"},{"role":"system","output_config":{"effort":"high"}}]}`, effort: "medium", present: true},
		{name: "tail_does_not_override_active_message", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"},{"role":"system","output_config":{"effort":"low"}},{"role":"assistant"}]}`, effort: "high", present: true},
		{name: "pending_without_user_stays_absent", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"assistant"}]}`},
		{name: "null_and_empty_top_level", body: `{"output_config":null,"messages":[{"role":"system","output_config":{}},{"role":"user"}]}`},
		{name: "empty_top_level", body: `{"output_config":{}}`},
		{name: "empty_and_null_do_not_clear_pending", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"system","output_config":{}},{"role":"system","output_config":null},{"role":"system"},{"role":"user"}]}`, effort: "high", present: true},
		{name: "empty_and_null_do_not_reset_active", body: `{"messages":[{"role":"system","output_config":{"effort":"high"}},{"role":"user"},{"role":"system","output_config":{}},{"role":"system","output_config":null},{"role":"user"}]}`, effort: "high", present: true},
		{name: "opaque_fields_do_not_set_effort", body: `{"output_config":{"extension":{"enabled":false}},"messages":[{"role":"assistant","output_config":{"format":{"type":"json"}}},{"role":"user"}]}`},
		{name: "future_value_is_preserved", body: `{"output_config":{"effort":" Future-Level "}}`, effort: " Future-Level ", present: true},
		{name: "does_not_validate_message_content", body: `{"messages":[{"role":"system","content":"retained","output_config":{"effort":"high"}},{"role":"user"}]}`, effort: "high", present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request ClaudeRequest
			require.NoError(t, kitutil.UnmarshalJsonStr(tc.body, &request))
			before, err := kitutil.Marshal(request)
			require.NoError(t, err)
			effort, present, err := request.ResolveEffectiveEffort()
			require.NoError(t, err)
			assert.Equal(t, tc.effort, effort)
			assert.Equal(t, tc.present, present)
			after, err := kitutil.Marshal(request)
			require.NoError(t, err)
			assert.Equal(t, before, after, "resolving effort must not change the wire request")
		})
	}
}

func TestClaudeResolveEffectiveEffortRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		path string
	}{
		{name: "malformed_json", raw: `{"effort":"private-value"`, path: "output_config"},
		{name: "trailing_json", raw: `{} {"effort":"private-value"}`, path: "output_config"},
		{name: "whitespace_only", raw: "  ", path: "output_config"},
		{name: "array", raw: `["private-value"]`, path: "output_config"},
		{name: "scalar", raw: `"private-value"`, path: "output_config"},
		{name: "number", raw: `42`, path: "output_config"},
		{name: "boolean", raw: `false`, path: "output_config"},
		{name: "null_effort", raw: `{"effort":null}`, path: "output_config.effort"},
		{name: "numeric_effort", raw: `{"effort":42}`, path: "output_config.effort"},
		{name: "boolean_effort", raw: `{"effort":false}`, path: "output_config.effort"},
		{name: "object_effort", raw: `{"effort":{"private-value":1}}`, path: "output_config.effort"},
		{name: "array_effort", raw: `{"effort":["private-value"]}`, path: "output_config.effort"},
		{name: "empty_effort", raw: `{"effort":""}`, path: "output_config.effort"},
		{name: "whitespace_effort", raw: `{"effort":" \t\n"}`, path: "output_config.effort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, message := range []bool{false, true} {
				request := ClaudeRequest{OutputConfig: json.RawMessage(tc.raw)}
				path := tc.path
				if message {
					request.OutputConfig = json.RawMessage(`{"effort":"medium"}`)
					request.Messages = []ClaudeMessage{{Role: "system", OutputConfig: json.RawMessage(tc.raw)}}
					path = "messages[0]." + path
				} else {
					request.Messages = []ClaudeMessage{{Role: "system", OutputConfig: json.RawMessage(`{"effort":"high"}`)}, {Role: "user"}}
				}
				effort, present, err := request.ResolveEffectiveEffort()
				require.ErrorContains(t, err, path)
				assert.NotContains(t, err.Error(), "private-value")
				assert.Empty(t, effort)
				assert.False(t, present)
				if message {
					assert.Equal(t, tc.raw, string(request.Messages[0].OutputConfig))
				} else {
					assert.Equal(t, tc.raw, string(request.OutputConfig))
				}
			}
		})
	}
}

func TestClaudeResolveEffectiveEffortRejectsUnsupportedRole(t *testing.T) {
	for _, role := range []string{"user", "assistant", "tool", "", "private-role"} {
		t.Run(role, func(t *testing.T) {
			request := ClaudeRequest{Messages: []ClaudeMessage{{Role: role, OutputConfig: json.RawMessage(`{"effort":"private-value"}`)}}}
			effort, present, err := request.ResolveEffectiveEffort()
			require.ErrorContains(t, err, "messages[0].role")
			assert.NotContains(t, err.Error(), "private-role")
			assert.NotContains(t, err.Error(), "private-value")
			assert.Empty(t, effort)
			assert.False(t, present)
		})
	}
}

func TestClaudeResolveEffectiveEffortNilRequest(t *testing.T) {
	var request *ClaudeRequest
	effort, present, err := request.ResolveEffectiveEffort()
	require.NoError(t, err)
	assert.Empty(t, effort)
	assert.False(t, present)
}

func TestClaudeResolveEffectiveEffortCopyAndRetryIsolation(t *testing.T) {
	var original ClaudeRequest
	require.NoError(t, kitutil.UnmarshalJsonStr(`{"output_config":{"effort":"medium"},"messages":[{"role":"system","output_config":{"effort":"high","opaque":{"count":9007199254740993}}},{"role":"user"}]}`, &original))
	before, err := kitutil.Marshal(original)
	require.NoError(t, err)
	copy, err := kitutil.Any2Type[ClaudeRequest](original)
	require.NoError(t, err)
	copy.Messages[0].OutputConfig = json.RawMessage(`{"effort":"low"}`)
	for _, tc := range []struct {
		request *ClaudeRequest
		effort  string
	}{{&copy, "low"}, {&original, "high"}, {&original, "high"}} {
		effort, present, err := tc.request.ResolveEffectiveEffort()
		require.NoError(t, err)
		assert.True(t, present)
		assert.Equal(t, tc.effort, effort)
	}
	assert.Equal(t, "medium", original.GetEfforts(), "the legacy accessor remains top-level only")
	after, err := kitutil.Marshal(original)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}
