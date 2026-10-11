package relayconvert

import (
	"context"
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
