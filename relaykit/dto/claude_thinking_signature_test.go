package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/require"
)

// Strict Anthropic clients parse signature as a required thinking-block field.
func TestClaudeThinkingBlockAlwaysCarriesSignatureKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		block ClaudeMediaMessage
		want  string
	}{
		{"unknown signature", ClaudeMediaMessage{Type: "thinking", Thinking: kitutil.GetPointer("plan")}, `{"type":"thinking","thinking":"plan","signature":""}`},
		{"stream block start", ClaudeMediaMessage{Type: "thinking"}, `{"type":"thinking","thinking":"","signature":""}`},
		{"known signature", ClaudeMediaMessage{Type: "thinking", Thinking: kitutil.GetPointer("plan"), Signature: "sig_provider"}, `{"type":"thinking","thinking":"plan","signature":"sig_provider"}`},
		{"other fields kept", ClaudeMediaMessage{Type: "thinking", Thinking: kitutil.GetPointer("plan"), CacheControl: []byte(`{"type":"ephemeral"}`)}, `{"type":"thinking","thinking":"plan","signature":"","cache_control":{"type":"ephemeral"}}`},
		{"text unchanged", ClaudeMediaMessage{Type: "text", Text: kitutil.GetPointer("hi")}, `{"type":"text","text":"hi"}`},
		{"tool use unchanged", ClaudeMediaMessage{Type: "tool_use", Id: "toolu_1", Name: "lookup", Input: map[string]any{"q": "x"}}, `{"type":"tool_use","id":"toolu_1","name":"lookup","input":{"q":"x"}}`},
		{"signature delta unchanged", ClaudeMediaMessage{Type: "signature_delta", Signature: "sig_provider"}, `{"type":"signature_delta","signature":"sig_provider"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := kitutil.Marshal(tc.block)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(encoded))

			var decoded ClaudeMediaMessage
			require.NoError(t, kitutil.Unmarshal(encoded, &decoded))
			require.Equal(t, tc.block.Signature, decoded.Signature)
		})
	}

	encoded, err := kitutil.Marshal(ClaudeResponse{Type: "content_block_start", ContentBlock: &ClaudeMediaMessage{Type: "thinking", Thinking: kitutil.GetPointer("")}})
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"content_block":{"type":"thinking","thinking":"","signature":""}`)
}
