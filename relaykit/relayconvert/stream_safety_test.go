package relayconvert

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestStreamTerminalEvidenceRejectsTruncationAndAcceptsEmptyCompletion(t *testing.T) {
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatGemini, types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
		t.Run(string(target), func(t *testing.T) {
			state, err := NewResponseStreamState(types.RelayFormatOpenAI, target, ResponseStreamOptions{RequireTerminal: true})
			require.NoError(t, err)
			_, err = ConvertStreamResponseChunk(nil, nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{Role: "assistant"}}}})
			require.NoError(t, err)
			assert.False(t, state.SawTerminalEvent())
			_, err = FinalizeStreamResponse(nil, nil, state)
			require.ErrorIs(t, err, ErrIncompleteStream)
			_, err = ConvertStreamResponseChunk(nil, nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: kitutil.GetPointer("stop")}}})
			require.NoError(t, err)
			assert.True(t, state.SawTerminalEvent())
			_, err = FinalizeStreamResponse(nil, nil, state)
			require.NoError(t, err)
		})
	}
}

func TestChatToGeminiStreamsOneCompleteToolCallAndRejectsInvalidArguments(t *testing.T) {
	for _, test := range []struct {
		name, tail string
		wantError  bool
	}{{"complete", `1}`, false}, {"truncated", "", true}} {
		t.Run(test.name, func(t *testing.T) {
			state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{RequireTerminal: true})
			require.NoError(t, err)
			chunks, err := ConvertStreamResponseChunk(nil, nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{Index: kitutil.GetPointer(0), ID: "call-fixed", Function: dto.FunctionResponse{Name: "lookup", Arguments: `{"q":`}}}}}}})
			require.NoError(t, err)
			assert.Empty(t, chunks, "partial Gemini arguments must not be emitted")
			chunks, err = ConvertStreamResponseChunk(nil, nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: kitutil.GetPointer("tool_calls"), Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{Index: kitutil.GetPointer(0), Function: dto.FunctionResponse{Arguments: test.tail}}}}}}})
			if test.wantError {
				require.Error(t, err)
				assert.Nil(t, chunks)
				return
			}
			require.NoError(t, err)
			require.Len(t, chunks, 1)
			value := chunks[0].Value.(*dto.GeminiChatResponse)
			require.Len(t, value.Candidates, 1)
			require.Len(t, value.Candidates[0].Content.Parts, 1)
			call := value.Candidates[0].Content.Parts[0].FunctionCall
			require.NotNil(t, call)
			assert.Equal(t, "call-fixed", call.ID)
			assert.Equal(t, map[string]any{"q": float64(1)}, call.Arguments)
			tail, err := FinalizeStreamResponse(nil, nil, state)
			require.NoError(t, err)
			assert.Empty(t, tail)
		})
	}
}

func TestGeminiConversionRejectsOversizedPrivateToolBuffer(t *testing.T) {
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatGemini, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(nil, nil, state, &dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{Function: dto.FunctionResponse{Name: "lookup", Arguments: strings.Repeat("x", 8*1024*1024+1)}}}}}}})
	require.ErrorContains(t, err, "8 MiB")
}

func TestRequestDiagnosticsBoundDoesNotHideRejectedLoss(t *testing.T) {
	tools := make([]map[string]any, 40)
	for i := range tools {
		tools[i] = map[string]any{"codeExecution": map[string]any{}}
	}
	raw, err := kitutil.Marshal(tools)
	require.NoError(t, err)
	req := &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hello"}}}}, Tools: raw}
	info := &convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe}}
	result, err := ConvertRequest(nil, info, types.RelayFormatOpenAI, req)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	require.NotNil(t, result)
	assert.Len(t, result.Diagnostics, 32)
	assert.True(t, result.DiagnosticsTruncated)
}
