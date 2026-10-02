package oairesponses

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomInputSnapshotSurvivesBufferedPartialTerminal(t *testing.T) {
	acc := NewResponsesBufferedAccumulator()
	for _, raw := range []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"custom_tool_call","id":"item","call_id":"call","name":"exec","input":""}}`,
		`{"type":"response.custom_tool_call_input.delta","output_index":0,"item_id":"item","delta":"old"}`,
		`{"type":"response.custom_tool_call_input.done","output_index":0,"item_id":"item","input":"authoritative"}`,
	} {
		var event dto.ResponsesStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(raw), &event))
		acc.ProcessEvent(&event)
		require.NoError(t, acc.Err())
	}
	terminal := &dto.OpenAIResponsesResponse{Output: []dto.ResponsesOutput{{ID: "item", Type: "custom_tool_call", Status: "completed"}}}
	acc.SupplementResponseOutput(terminal)
	require.NoError(t, acc.Err())
	require.Len(t, terminal.Output, 1)
	assert.Equal(t, `"authoritative"`, string(terminal.Output[0].Input))
	assert.Equal(t, "authoritative", terminal.Output[0].ArgumentsString())
	encoded, err := kitutil.Marshal(terminal.Output[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"custom_tool_call","id":"item","status":"completed","call_id":"call","name":"exec","input":"authoritative"}`, string(encoded))
}

func TestToolDoneSnapshotCompletesDeltasOrRejectsConflict(t *testing.T) {
	for _, custom := range []bool{false, true} {
		for _, conflict := range []bool{false, true} {
			state := NewResponsesToChatStreamState("model", false)
			kind, deltaType, doneType := "function_call", responsesEventFunctionArgsDelta, responsesEventFunctionArgsDone
			if custom {
				kind, deltaType, doneType = "custom_tool_call", responsesEventCustomToolInputDelta, responsesEventCustomToolInputDone
			}
			_, err := ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{Type: responsesEventOutputItemAdded, OutputIndex: kitutil.GetPointer(0), Item: &dto.ResponsesOutput{Type: kind, ID: "item", CallId: "call", Name: "tool"}}, state)
			require.NoError(t, err)
			_, err = ResponsesStreamEventToChatChunks(&dto.ResponsesStreamResponse{Type: deltaType, OutputIndex: kitutil.GetPointer(0), ItemID: "item", Delta: "prefix"}, state)
			require.NoError(t, err)
			final := "prefix-tail"
			if conflict {
				final = "changed"
			}
			event := &dto.ResponsesStreamResponse{Type: doneType, OutputIndex: kitutil.GetPointer(0), ItemID: "item"}
			if custom {
				event.Input = &final
			} else {
				event.Arguments = &final
			}
			chunks, err := ResponsesStreamEventToChatChunks(event, state)
			if conflict {
				require.ErrorContains(t, err, "conflict")
				continue
			}
			require.NoError(t, err)
			require.Len(t, chunks, 1)
			assert.Equal(t, "-tail", chunks[0].Choices[0].Delta.ToolCalls[0].Function.Arguments)
		}
	}
}
