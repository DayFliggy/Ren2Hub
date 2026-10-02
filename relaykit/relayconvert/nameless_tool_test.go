package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNamelessChatToolCannotBecomeCompletedResponse(t *testing.T) {
	for _, name := range []string{"", "   "} {
		t.Run(name, func(t *testing.T) {
			message := dto.Message{Role: "assistant", Content: "Partial output before the invalid tool"}
			message.SetToolCalls([]dto.ToolCallRequest{{ID: "call_1", Type: "function", Function: dto.FunctionRequest{Name: name, Arguments: `{}`}}})
			input := &dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}}, Usage: dto.Usage{PromptTokens: 5, CompletionTokens: 2, TotalTokens: 7}}
			result, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAIResponses, input)
			require.ErrorContains(t, err, "missing function name")
			if result != nil && result.Value != nil {
				body, marshalErr := kitutil.Marshal(result.Value)
				require.NoError(t, marshalErr)
				assert.NotContains(t, string(body), `"completed"`)
			}
		})
	}
}
