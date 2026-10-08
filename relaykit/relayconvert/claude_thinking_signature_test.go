package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// Generated thinking blocks have no provider signature. They must still carry
// the key, and the value must stay empty rather than a fabricated token.
func TestGeneratedClaudeThinkingBlocksCarryEmptySignature(t *testing.T) {
	chat := &dto.OpenAITextResponse{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"id":"chatcmpl_1","choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"plan"},"finish_reason":"stop"}]}`), chat))
	responses := &dto.OpenAIResponsesResponse{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"id":"resp_1","status":"completed","output":[{"type":"reasoning","summary":[{"type":"summary_text","text":"plan"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}]}`), responses))
	for _, response := range []any{chat, responses} {
		result, err := ConvertResponse(context.Background(), nil, types.RelayFormatClaude, response)
		require.NoError(t, err)
		encoded, err := kitutil.Marshal(result.Value)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `{"type":"thinking","thinking":"plan","signature":""}`)
	}

	chatChunk := &dto.ChatCompletionsStreamResponse{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"id":"chatcmpl_1","choices":[{"index":0,"delta":{"reasoning_content":"plan"}}]}`), chatChunk))
	responsesChunk := &dto.ResponsesStreamResponse{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"type":"response.reasoning_summary_text.delta","output_index":0,"item_id":"rs_1","delta":"plan"}`), responsesChunk))
	for _, stream := range []struct {
		from  types.RelayFormat
		chunk any
	}{{types.RelayFormatOpenAI, chatChunk}, {types.RelayFormatOpenAIResponses, responsesChunk}} {
		state, err := NewResponseStreamState(stream.from, types.RelayFormatClaude, ResponseStreamOptions{})
		require.NoError(t, err)
		results, err := ConvertStreamResponseChunk(context.Background(), nil, state, stream.chunk)
		require.NoError(t, err)
		encoded, err := kitutil.Marshal(streamValues(results))
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"content_block":{"type":"thinking","thinking":"","signature":""}`, stream.from)
	}
}

func streamValues(results []ResponseResult) []any {
	values := make([]any, 0, len(results))
	for _, result := range results {
		values = append(values, result.Value)
	}
	return values
}
