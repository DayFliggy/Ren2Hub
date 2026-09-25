package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVLLMThinkingTokenBudgetPreservesPresence(t *testing.T) {
	for _, body := range []string{`{"model":"served-model"}`, `{"model":"served-model","thinking_token_budget":0}`, `{"model":"served-model","thinking_token_budget":8192}`} {
		var request GeneralOpenAIRequest
		require.NoError(t, json.Unmarshal([]byte(body), &request))
		encoded, err := json.Marshal(request)
		require.NoError(t, err)
		var before, after map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(body), &before))
		require.NoError(t, json.Unmarshal(encoded, &after))
		assert.Equal(t, before["thinking_token_budget"], after["thinking_token_budget"])
	}
}

func TestKimiDynamicToolMessagesRetainWireShapeAndTokenMetadata(t *testing.T) {
	body := `{"model":"kimi-k3","messages":[{"role":"system","tools":[{"type":"function","function":{"name":"dynamic_lookup","description":"visible tool","parameters":{"type":"object"}}}]},{"role":"assistant","content":null},{"role":"system","content":"kept","tools":[{"type":"function","function":{"name":"second"}}]},{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"static_lookup"}}]}`
	var request GeneralOpenAIRequest
	require.NoError(t, json.Unmarshal([]byte(body), &request))
	encoded, err := json.Marshal(request)
	require.NoError(t, err)
	var wire struct {
		Messages []map[string]json.RawMessage `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(encoded, &wire))
	assert.NotContains(t, wire.Messages[0], "content")
	assert.NotEmpty(t, wire.Messages[0]["tools"])
	assert.Equal(t, "null", string(wire.Messages[1]["content"]))
	assert.Equal(t, `"kept"`, string(wire.Messages[2]["content"]))
	meta := request.GetTokenCountMeta()
	assert.Equal(t, 3, meta.ToolsCount)
	for _, value := range []string{"dynamic_lookup", "visible tool", "second", "static_lookup"} {
		assert.Contains(t, meta.CombineText, value)
	}
	assert.Nil(t, request.Messages[0].Content)
}
