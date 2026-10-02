package claudemessages

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeToolInputSeedIsFallbackToDeltas(t *testing.T) {
	for _, test := range []struct {
		name   string
		seed   any
		deltas []string
		want   string
	}{
		{"complete seed", map[string]any{"q": "seed"}, nil, `{"q":"seed"}`},
		{"empty seed", map[string]any{}, nil, `{}`},
		{"empty seed then deltas", map[string]any{}, []string{`{"q":`, `"delta"}`}, `{"q":"delta"}`},
		{"complete seed then deltas", map[string]any{"q": "seed"}, []string{`{"q":`, `"delta"}`}, `{"q":"delta"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := NewClaudeToChatStreamState()
			index := 3
			start, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_start", Index: &index, ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use", Id: "call_1", Name: "lookup", Input: test.seed}})
			require.NoError(t, err)
			require.NotNil(t, start)
			assert.Empty(t, start.Choices[0].Delta.ToolCalls[0].Function.Arguments, "seed must not be emitted before knowing whether deltas follow")
			var arguments strings.Builder
			for _, delta := range test.deltas {
				chunk, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_delta", Index: &index, Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: &delta}})
				require.NoError(t, err)
				arguments.WriteString(chunk.Choices[0].Delta.ToolCalls[0].Function.Arguments)
			}
			stop, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_stop", Index: &index})
			require.NoError(t, err)
			if stop != nil {
				arguments.WriteString(stop.Choices[0].Delta.ToolCalls[0].Function.Arguments)
			}
			assert.JSONEq(t, test.want, arguments.String())
			again, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_stop", Index: &index})
			require.NoError(t, err)
			assert.Nil(t, again, "a repeated block stop cannot emit the seed twice")
		})
	}
}

func TestClaudeInterleavedToolSeedsKeepTheirOwnArguments(t *testing.T) {
	state := NewClaudeToChatStreamState()
	for _, index := range []int{2, 5} {
		_, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_start", Index: &index, ContentBlock: &dto.ClaudeMediaMessage{Type: "tool_use", Id: "call", Name: "lookup", Input: map[string]any{"index": index}}})
		require.NoError(t, err)
	}
	_, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_delta", Index: kitutil.GetPointer(5), Delta: &dto.ClaudeMediaMessage{Type: "input_json_delta", PartialJson: kitutil.GetPointer(`{"delta":true}`)}})
	require.NoError(t, err)
	second, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_stop", Index: kitutil.GetPointer(5)})
	require.NoError(t, err)
	assert.Nil(t, second)
	first, err := state.ConvertChunk(&dto.ClaudeResponse{Type: "content_block_stop", Index: kitutil.GetPointer(2)})
	require.NoError(t, err)
	require.NotNil(t, first)
	tool := first.Choices[0].Delta.ToolCalls[0]
	assert.Equal(t, 0, *tool.Index)
	assert.JSONEq(t, `{"index":2}`, tool.Function.Arguments)
}
