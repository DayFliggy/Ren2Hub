package claudemessages

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponseClaude2OpenAIPreservesAllContentBlocks(t *testing.T) {
	response := &dto.ClaudeResponse{
		Id: "msg_mixed", Model: "claude-test", StopReason: "tool_use",
		Content: []dto.ClaudeMediaMessage{
			{Type: "thinking", Thinking: kitutil.GetPointer("first thought ")},
			{Type: "text", Text: kitutil.GetPointer("first ")},
			{Type: "tool_use", Id: "call_1", Name: "lookup", Input: map[string]any{"q": "one"}},
			{Type: "thinking", Thinking: kitutil.GetPointer("second thought")},
			{Type: "text", Text: kitutil.GetPointer("second")},
			{Type: "tool_use", Id: "call_2", Name: "lookup", Input: map[string]any{"q": "two"}},
		},
	}
	converted := ResponseClaude2OpenAI(response)
	require.Len(t, converted.Choices, 1)
	choice := converted.Choices[0]
	assert.Equal(t, "first second", choice.Message.StringContent())
	require.NotNil(t, choice.Message.ReasoningContent)
	assert.Equal(t, "first thought second thought", *choice.Message.ReasoningContent)
	assert.Equal(t, "tool_calls", choice.FinishReason)
	tools := choice.Message.ToolCalls
	encoded, err := kitutil.Marshal(tools)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"one\"}"}},{"id":"call_2","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"two\"}"}}]`, string(encoded))
}

func TestClaudeToChatStreamUsesDenseStableToolIndexes(t *testing.T) {
	var state ClaudeToChatStreamState
	events := []struct {
		data      string
		toolIndex int
		arguments string
	}{
		{`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`, -1, ""},
		{`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"checking"}}`, -1, ""},
		{`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"first","name":"lookup","input":{}}}`, 0, ""},
		{`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":"}}`, 0, `{"q":`},
		{`{"type":"content_block_start","index":3,"content_block":{"type":"text","text":"then"}}`, -1, ""},
		{`{"type":"content_block_start","index":4,"content_block":{"type":"tool_use","id":"second","name":"lookup","input":{}}}`, 1, ""},
		{`{"type":"content_block_delta","index":4,"delta":{"type":"input_json_delta","partial_json":"{\"q\":2}"}}`, 1, `{"q":2}`},
		{`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"1}"}}`, 0, `1}`},
	}
	for _, event := range events {
		var response dto.ClaudeResponse
		require.NoError(t, kitutil.Unmarshal([]byte(event.data), &response))
		converted, err := state.ConvertChunk(&response)
		require.NoError(t, err)
		require.NotNil(t, converted)
		require.Len(t, converted.Choices, 1)
		tools := converted.Choices[0].Delta.ToolCalls
		if event.toolIndex < 0 {
			assert.Empty(t, tools)
			continue
		}
		require.Len(t, tools, 1)
		require.NotNil(t, tools[0].Index)
		assert.Equal(t, event.toolIndex, *tools[0].Index)
		assert.Equal(t, event.arguments, tools[0].Function.Arguments)
		assert.NotEqual(t, *response.Index, *tools[0].Index, "conversion must not rewrite the upstream content index")
	}
	var secondStream ClaudeToChatStreamState
	response := &dto.ClaudeResponse{Type: "content_block_start", Index: kitutil.GetPointer(9), ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use", Id: "fresh", Name: "lookup"}}
	converted, err := secondStream.ConvertChunk(response)
	require.NoError(t, err)
	assert.Equal(t, 0, *converted.Choices[0].Delta.ToolCalls[0].Index)
}

func TestClaudeToChatStreamRejectsUnassociatedToolArguments(t *testing.T) {
	for _, data := range []string{
		`{"type":"content_block_start","content_block":{"type":"tool_use","id":"a","name":"f"}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta"}}`,
	} {
		var state ClaudeToChatStreamState
		var response dto.ClaudeResponse
		require.NoError(t, kitutil.Unmarshal([]byte(data), &response))
		converted, err := state.ConvertChunk(&response)
		require.Error(t, err)
		assert.Nil(t, converted)
	}
}

func TestClaudeStreamBillingPreservesAuthoritativeSnapshots(t *testing.T) {
	for _, stage := range []string{"initial", "message_start", "message_delta"} {
		t.Run(stage, func(t *testing.T) {
			authoritative := dto.NewOpenAIChatBillingUsage(&dto.Usage{PromptTokens: 42, CompletionTokens: 7, TotalTokens: 49})
			info := &ClaudeResponseInfo{Usage: &dto.Usage{}}
			start := &dto.ClaudeUsage{InputTokens: 10}
			delta := &dto.ClaudeUsage{OutputTokens: 1}
			switch stage {
			case "initial":
				info.Usage.BillingUsage = authoritative
			case "message_start":
				start.BillingUsage = authoritative
			case "message_delta":
				delta.BillingUsage = authoritative
			}
			require.True(t, FormatClaudeResponseInfo(&dto.ClaudeResponse{Type: "message_start", Message: &dto.ClaudeMediaMessage{Usage: start}}, nil, info))
			require.True(t, FormatClaudeResponseInfo(&dto.ClaudeResponse{Type: "message_delta", Usage: delta}, nil, info))
			info.Usage.CompletionTokens = 99
			info.Usage.UsageSource = "estimated_partial"
			FinalizeClaudeStreamBillingUsage(info)
			assert.Equal(t, authoritative, info.Usage.BillingUsage)
			assert.False(t, info.Usage.BillingUsage.Estimated)
			FinalizeClaudeStreamBillingUsage(info)
			assert.Equal(t, authoritative, info.Usage.BillingUsage)
		})
	}
}
