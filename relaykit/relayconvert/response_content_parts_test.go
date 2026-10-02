package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesContentPartLifecycleAfterTextAndImageConversion(t *testing.T) {
	for _, image := range []bool{false, true} {
		name := "text"
		content := []any{map[string]any{"type": "input_text", "text": "describe"}}
		if image {
			name = "image"
			content = append(content, map[string]any{"type": "input_image", "image_url": "data:image/png;base64,aGVsbG8="})
		}
		t.Run(name, func(t *testing.T) {
			input, err := kitutil.Marshal([]any{map[string]any{"role": "user", "content": content}})
			require.NoError(t, err)
			meta := &convmeta.Values{}
			request, err := ConvertRequest(context.Background(), meta, types.RelayFormatClaude, &dto.OpenAIResponsesRequest{Model: "model", Input: input, MaxOutputTokens: kitutil.GetPointer(uint(64))})
			require.NoError(t, err)
			claude := request.Value.(*dto.ClaudeRequest)
			parts, err := claude.Messages[0].ParseContent()
			require.NoError(t, err)
			require.Len(t, parts, len(content))
			if image {
				assert.Equal(t, "image", parts[1].Type)
			}
			state, err := NewResponseStreamState(types.RelayFormatClaude, types.RelayFormatOpenAIResponses, ResponseStreamOptions{ID: "resp_parts", Model: "model", EmitSequenceNumber: true})
			require.NoError(t, err)
			var events []ChatToResponsesStreamEvent
			for _, frame := range []string{
				`{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","model":"model","usage":{"input_tokens":10,"output_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
				`{"type":"message_stop"}`,
			} {
				var response dto.ClaudeResponse
				require.NoError(t, kitutil.Unmarshal([]byte(frame), &response))
				results, err := ConvertStreamResponseChunk(context.Background(), meta, state, &response)
				require.NoError(t, err)
				for _, result := range results {
					events = append(events, result.Value.(ChatToResponsesStreamEvent))
				}
			}
			finals, err := FinalizeStreamResponse(context.Background(), meta, state)
			require.NoError(t, err)
			for _, result := range finals {
				events = append(events, result.Value.(ChatToResponsesStreamEvent))
			}
			names := make([]string, 0, len(events))
			for index, event := range events {
				names = append(names, event.Type)
				require.NotNil(t, event.Payload.SequenceNumber)
				assert.Equal(t, index, *event.Payload.SequenceNumber)
			}
			assert.Equal(t, []string{"response.created", "response.output_item.added", "response.content_part.added", "response.output_text.delta", "response.output_text.done", "response.content_part.done", "response.output_item.done", "response.completed"}, names)
			require.Len(t, events, 8)
			for _, index := range []int{2, 3, 4, 5} {
				assert.Equal(t, "resp_parts_msg_0", events[index].Payload.ItemID)
				assert.Equal(t, 0, *events[index].Payload.OutputIndex)
				assert.Equal(t, 0, *events[index].Payload.ContentIndex)
			}
			added, err := kitutil.Marshal(events[2].Payload.Part)
			require.NoError(t, err)
			assert.JSONEq(t, `{"type":"output_text","text":"","annotations":[]}`, string(added))
			assert.Equal(t, "hello", *events[4].Payload.Text)
			assert.Equal(t, "hello", events[5].Payload.Part.Text)
			assert.Equal(t, events[5].Payload.Part.Text, events[6].Payload.Item.Content[0].Text)
			assert.Equal(t, []dto.ResponsesOutput{*events[6].Payload.Item}, events[7].Payload.Response.Output)
		})
	}
}
