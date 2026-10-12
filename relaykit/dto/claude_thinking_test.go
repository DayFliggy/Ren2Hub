package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeThinkingPreservesNativeExtensions(t *testing.T) {
	const original = `{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future":{"enabled":false,"limit":0},"unset":null}`
	var thinking Thinking
	require.NoError(t, kitutil.Unmarshal([]byte(original), &thinking))
	encoded, err := kitutil.Marshal(thinking)
	require.NoError(t, err)
	assert.JSONEq(t, original, string(encoded))

	// Reusing a decoded DTO must not retain fields absent from the next input.
	require.NoError(t, kitutil.Unmarshal([]byte(`{"type":"disabled"}`), &thinking))
	encoded, err = kitutil.Marshal(thinking)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"disabled"}`, string(encoded))
}

func TestClaudeThinkingRejectsMalformedInput(t *testing.T) {
	for _, body := range []string{`{"type":false}`, `{"block_binding":`, `[]`} {
		var thinking Thinking
		assert.Error(t, kitutil.Unmarshal([]byte(body), &thinking), body)
	}
}
