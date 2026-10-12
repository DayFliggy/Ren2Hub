package relayconvert

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A client may put a message between a call and its output. Claude and Gemini
// reject a tool result that does not directly follow its call, so the output
// moves up to its call; an output for a later call stays where it was.
const responsesInterleavedToolOutputs = `[
	{"role":"user","content":"start"},
	{"type":"function_call","call_id":"call_a","name":"lookup","arguments":"{}"},
	{"type":"function_call","call_id":"call_b","name":"lookup","arguments":"{}"},
	{"role":"user","content":"context"},
	{"type":"function_call_output","call_id":"call_b","output":"b"},
	{"type":"function_call","call_id":"call_c","name":"lookup","arguments":"{}"},
	{"type":"function_call_output","call_id":"call_a","output":"a"},
	{"type":"function_call_output","call_id":"call_c","output":"c"}
]`

func TestResponsesToolOutputsFollowTheirCallsForClaude(t *testing.T) {
	maxTokens := uint(64)
	got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
		Model: "claude-test", MaxOutputTokens: &maxTokens, Input: []byte(responsesInterleavedToolOutputs),
	})
	require.NoError(t, err)
	type turn struct {
		Role  string
		Parts []string
	}
	var turns []turn
	for _, message := range got.Messages {
		parts, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](message.Content)
		require.NoError(t, err)
		summary := turn{Role: message.Role}
		for _, part := range parts {
			switch part.Type {
			case "tool_use":
				summary.Parts = append(summary.Parts, "use:"+part.Id)
			case "tool_result":
				summary.Parts = append(summary.Parts, "result:"+part.ToolUseId)
			default:
				summary.Parts = append(summary.Parts, part.GetText())
			}
		}
		turns = append(turns, summary)
	}
	assert.Equal(t, []turn{
		{"user", []string{"start"}},
		{"assistant", []string{"use:call_a", "use:call_b"}},
		{"user", []string{"result:call_b", "result:call_a"}},
		{"user", []string{"context"}},
		{"assistant", []string{"use:call_c"}},
		{"user", []string{"result:call_c"}},
	}, turns)
}

func TestResponsesToolOutputsFollowTheirCallsForGemini(t *testing.T) {
	got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
		Model: "gemini-test", Input: []byte(responsesInterleavedToolOutputs),
	}, nil)
	require.NoError(t, err)
	type turn struct {
		Role  string
		Parts []string
	}
	var turns []turn
	for _, content := range got.Contents {
		summary := turn{Role: content.Role}
		for _, part := range content.Parts {
			switch {
			case part.FunctionCall != nil:
				summary.Parts = append(summary.Parts, "call:"+part.FunctionCall.FunctionName)
			case part.FunctionResponse != nil:
				summary.Parts = append(summary.Parts, "response:"+part.FunctionResponse.Name)
			default:
				summary.Parts = append(summary.Parts, part.Text)
			}
		}
		turns = append(turns, summary)
	}
	assert.Equal(t, []turn{
		{"user", []string{"start"}},
		{"model", []string{"call:lookup", "call:lookup"}},
		{"user", []string{"response:lookup", "response:lookup"}},
		{"user", []string{"context"}},
		{"model", []string{"call:lookup"}},
		{"user", []string{"response:lookup"}},
	}, turns)
}

// Reasoning items interleaved with parallel calls belong to the same batch:
// every target keeps one assistant turn with both calls and one tool turn.
const responsesReasoningInterleavedCalls = `[
	{"role":"user","content":"start"},
	{"type":"reasoning","summary":[{"type":"summary_text","text":"plan a"}]},
	{"type":"function_call","call_id":"call_a","name":"lookup","arguments":"{}"},
	{"type":"reasoning","summary":[{"type":"summary_text","text":"plan b"}]},
	{"type":"function_call","call_id":"call_b","name":"lookup","arguments":"{}"},
	{"type":"function_call_output","call_id":"call_a","output":"a"},
	{"type":"function_call_output","call_id":"call_b","output":"b"}
]`

func TestResponsesReasoningBetweenCallsKeepsOneBatch(t *testing.T) {
	t.Run("chat", func(t *testing.T) {
		got, err := ResponsesRequestToChatCompletionsRequest(&dto.OpenAIResponsesRequest{
			Model: "gpt-test", Input: []byte(responsesReasoningInterleavedCalls),
		})
		require.NoError(t, err)
		var turns []string
		for _, message := range got.Messages {
			turn := message.Role + ":" + message.ToolCallId
			for _, call := range message.ParseToolCalls() {
				turn += " " + call.ID
			}
			turns = append(turns, turn)
		}
		assert.Equal(t, []string{"user:", "assistant: call_a call_b", "tool:call_a", "tool:call_b"}, turns)
	})

	t.Run("claude", func(t *testing.T) {
		maxTokens := uint(64)
		got, err := OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &dto.OpenAIResponsesRequest{
			Model: "claude-test", MaxOutputTokens: &maxTokens, Input: []byte(responsesReasoningInterleavedCalls),
		})
		require.NoError(t, err)
		var turns []string
		for _, message := range got.Messages {
			parts, err := kitutil.Any2Type[[]dto.ClaudeMediaMessage](message.Content)
			require.NoError(t, err)
			turn := message.Role + ":"
			for _, part := range parts {
				turn += " " + part.Type + part.Id + part.ToolUseId
			}
			turns = append(turns, turn)
		}
		assert.Equal(t, []string{
			"user: text",
			"assistant: tool_usecall_a tool_usecall_b",
			"user: tool_resultcall_a tool_resultcall_b",
		}, turns)
	})

	t.Run("gemini", func(t *testing.T) {
		got, err := OpenAIResponsesRequestToGeminiChat(context.Background(), &dto.OpenAIResponsesRequest{
			Model: "gemini-test", Input: []byte(responsesReasoningInterleavedCalls),
		}, nil)
		require.NoError(t, err)
		var turns []string
		for _, content := range got.Contents {
			turns = append(turns, fmt.Sprintf("%s:%d", content.Role, len(content.Parts)))
		}
		assert.Equal(t, []string{"user:1", "model:2", "user:2"}, turns)
	})
}
