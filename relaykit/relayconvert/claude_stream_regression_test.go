package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatelessClaudeToolStreamRequiresState(t *testing.T) {
	result, err := ConvertStreamResponse(nil, nil, types.RelayFormatOpenAI, &dto.ClaudeResponse{
		Type: "content_block_start", Index: kitutil.GetPointer(2),
		ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use", Id: "call_1", Name: "lookup"},
	})
	require.ErrorContains(t, err, "per-stream state")
	assert.Nil(t, result)
}

func TestClaudeRegisteredStreamUsesDenseToolIndexes(t *testing.T) {
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		t.Run(string(target), func(t *testing.T) {
			state, err := NewResponseStreamState(types.RelayFormatClaude, target, ResponseStreamOptions{ID: "msg_1", Model: "claude-test"})
			require.NoError(t, err)
			info := &convmeta.Values{UpstreamModelName: "claude-test", ChannelMetaAttached: true}
			var results []ResponseResult
			for _, event := range []string{
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"checking"}}`,
				`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_1","name":"lookup","input":{}}}`,
				`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":1}"}}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`,
			} {
				var response dto.ClaudeResponse
				require.NoError(t, kitutil.Unmarshal([]byte(event), &response))
				chunk, err := ConvertStreamResponseChunk(nil, info, state, &response)
				require.NoError(t, err)
				results = append(results, chunk...)
			}
			final, err := FinalizeStreamResponse(nil, info, state)
			require.NoError(t, err)
			results = append(results, final...)
			encoded, err := kitutil.Marshal(results)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), "lookup")
			assert.Contains(t, string(encoded), "call_1")
			if target == types.RelayFormatOpenAI {
				for _, result := range results {
					chunk, ok := result.Value.(*dto.ChatCompletionsStreamResponse)
					require.True(t, ok)
					for _, tool := range chunk.Choices[0].Delta.ToolCalls {
						require.NotNil(t, tool.Index)
						assert.Zero(t, *tool.Index)
					}
				}
			}
		})
	}
}
