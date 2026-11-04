package geminichat

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContinuationMapsToLengthInStreamAndResponse(t *testing.T) {
	finish := "CONTINUATION"
	response := &dto.GeminiChatResponse{Candidates: []dto.GeminiChatCandidate{{
		Content:      dto.GeminiChatContent{Role: "model", Parts: []dto.GeminiPart{{Text: "partial"}}},
		FinishReason: &finish, ContinuationToken: json.RawMessage(`{"opaque":"next"}`),
	}}}
	nonstream := ResponseGeminiChat2OpenAI("test", 1, response)
	require.Len(t, nonstream.Choices, 1)
	assert.Equal(t, types.FinishReasonLength, nonstream.Choices[0].FinishReason)
	stream, stopped := StreamResponseGeminiChat2OpenAI(response)
	require.False(t, stopped, "CONTINUATION must retain its explicit length finish reason")
	require.Len(t, stream.Choices, 1)
	require.NotNil(t, stream.Choices[0].FinishReason)
	assert.Equal(t, types.FinishReasonLength, *stream.Choices[0].FinishReason)
	assert.JSONEq(t, `{"opaque":"next"}`, string(response.Candidates[0].ContinuationToken))
}
