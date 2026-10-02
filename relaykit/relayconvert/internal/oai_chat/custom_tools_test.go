package oaichat

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCustomToolStreamHoldsNameAndArgumentsUntilCallID(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_test", "model")
	state.EmitSequenceNumber = true
	state.Tools = &convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}}
	var events []ChatToResponsesStreamEvent
	for _, chunk := range []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"exec","arguments":"{\"input\":\"echo "}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_late","function":{"arguments":"\\\"ok\\\"\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		var response dto.ChatCompletionsStreamResponse
		require.NoError(t, kitutil.Unmarshal([]byte(chunk), &response))
		got, err := ChatCompletionsStreamChunkToResponsesEvents(&response, state)
		require.NoError(t, err)
		events = append(events, got...)
	}
	events = append(events, FinalizeChatCompletionsStreamToResponses(state)...)
	var names []string
	for index, event := range events {
		require.NotNil(t, event.Payload.SequenceNumber)
		assert.Equal(t, index, *event.Payload.SequenceNumber)
		names = append(names, event.Type)
		if event.Payload.Item != nil {
			assert.Equal(t, "call_late", event.Payload.Item.CallId)
			assert.Equal(t, "call_late", event.Payload.Item.ID)
		}
	}
	assert.Equal(t, []string{"response.created", "response.output_item.added", "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done", "response.output_item.done", "response.completed"}, names)
	assert.Equal(t, `echo "ok"`, events[2].Payload.Delta)
	require.NotNil(t, events[3].Payload.Input)
	assert.Equal(t, events[2].Payload.Delta, *events[3].Payload.Input)
	output := events[len(events)-1].Payload.Response.Output
	require.Len(t, output, 1)
	encoded, err := kitutil.Marshal(output[0])
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"custom_tool_call","id":"call_late","status":"completed","call_id":"call_late","name":"exec","input":"echo \"ok\""}`, string(encoded))
}

func TestCustomToolMalformedArgumentsNeverBecomeSuccess(t *testing.T) {
	for _, args := range []string{``, `exec`, `{"input":`, `{}`, `{"input":42}`, `{"input":"ok","extra":true}`} {
		t.Run(args, func(t *testing.T) {
			state := NewChatToResponsesStreamState("resp_test", "model")
			state.Tools = &convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}}
			call := dto.ToolCallResponse{Index: kitutil.GetPointer(0), ID: "call", Type: "function", Function: dto.FunctionResponse{Name: "exec", Arguments: args}}
			_, err := ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{call}}}}}, state)
			require.NoError(t, err)
			events := FinalizeChatCompletionsStreamToResponses(state)
			require.Error(t, state.ConversionError())
			require.NotEmpty(t, events)
			assert.Equal(t, "response.failed", events[len(events)-1].Type)
			for _, event := range events {
				assert.NotEqual(t, "response.completed", event.Type)
			}
			message := dto.Message{Role: "assistant"}
			message.SetToolCalls([]dto.ToolCallRequest{{ID: "call", Type: "function", Function: dto.FunctionRequest{Name: "exec", Arguments: args}}})
			_, _, err = ChatCompletionsResponseToResponsesResponseWithTools(&dto.OpenAITextResponse{Choices: []dto.OpenAITextResponseChoice{{Message: message, FinishReason: "tool_calls"}}}, "resp_test", state.Tools)
			require.Error(t, err)
		})
	}
}

func TestNamelessToolCannotSilentlyDisappearAtCompletion(t *testing.T) {
	state := NewChatToResponsesStreamState("resp_test", "model")
	_, err := ChatCompletionsStreamChunkToResponsesEvents(&dto.ChatCompletionsStreamResponse{Choices: []dto.ChatCompletionsStreamResponseChoice{{Delta: dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{{Index: kitutil.GetPointer(0), ID: "call", Function: dto.FunctionResponse{Arguments: `{}`}}}}}}}, state)
	require.NoError(t, err)
	events := FinalizeChatCompletionsStreamToResponses(state)
	require.ErrorContains(t, state.ConversionError(), "without a name")
	assert.Equal(t, "response.failed", events[len(events)-1].Type)
}
