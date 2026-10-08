package oairesponses

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesRefusalBecomesClaudeTextWithRefusalStopReason(t *testing.T) {
	refusal := "I can't help with that."
	refusalPart := dto.ResponsesOutputContent{Type: "refusal", Refusal: &refusal}
	toolCall := dto.ResponsesOutput{Type: responsesOutputTypeFunctionCall, ID: "fc_1", CallId: "call_1", Name: "lookup", Arguments: []byte(`"{\"q\":\"x\"}"`)}

	tests := []struct {
		name       string
		response   dto.OpenAIResponsesResponse
		wantBlocks []dto.ClaudeMediaMessage
	}{
		{
			name: "refusal only",
			response: dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{
				{Type: responsesOutputTypeMessage, Role: "assistant", Content: []dto.ResponsesOutputContent{refusalPart}},
			}},
			wantBlocks: []dto.ClaudeMediaMessage{{Type: "text", Text: kitutil.GetPointer(refusal)}},
		},
		{
			name: "refusal outranks a tool call and keeps content order",
			response: dto.OpenAIResponsesResponse{Status: []byte(`"completed"`), Output: []dto.ResponsesOutput{
				{Type: responsesOutputTypeMessage, Role: "assistant", Content: []dto.ResponsesOutputContent{{Type: "output_text", Text: "Partial answer."}, refusalPart}},
				toolCall,
			}},
			wantBlocks: []dto.ClaudeMediaMessage{
				{Type: "text", Text: kitutil.GetPointer("Partial answer.")},
				{Type: "text", Text: kitutil.GetPointer(refusal)},
				{Type: "tool_use", Id: "call_1", Name: "lookup", Input: map[string]any{"q": "x"}},
			},
		},
		{
			name: "refusal outranks an output limit",
			response: dto.OpenAIResponsesResponse{
				Status:            []byte(`"incomplete"`),
				IncompleteDetails: &dto.IncompleteDetails{Reason: responsesIncompleteReasonMaxTokens},
				Output: []dto.ResponsesOutput{
					{Type: responsesOutputTypeMessage, Role: "assistant", Content: []dto.ResponsesOutputContent{refusalPart}},
				},
			},
			wantBlocks: []dto.ClaudeMediaMessage{{Type: "text", Text: kitutil.GetPointer(refusal)}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response, _, err := ResponsesResponseToClaudeMessagesResponse(&tt.response)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBlocks, response.Content)
			assert.Equal(t, "refusal", response.StopReason)
		})
	}
}
