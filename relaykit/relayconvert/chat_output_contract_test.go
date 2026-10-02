package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestChatOutputFieldsRemainNativeAndRejectLossyConversion(t *testing.T) {
	for _, field := range []string{`"refusal":"declined"`, `"audio":{"id":"audio_1","data":"YQ=="}`} {
		var response dto.OpenAITextResponse
		require.NoError(t, kitutil.Unmarshal([]byte(`{"choices":[{"index":0,"message":{"role":"assistant",`+field+`},"finish_reason":"stop"}]}`), &response))
		native, err := ConvertResponse(context.Background(), nil, types.RelayFormatOpenAI, &response)
		require.NoError(t, err)
		encoded, err := kitutil.Marshal(native.Value)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), field)
		for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
			_, err := ConvertResponse(context.Background(), nil, target, &response)
			require.ErrorContains(t, err, "choices[0].message")
		}
	}
}

func TestChatStreamDTOAndNativeRoutePreserveOutputFields(t *testing.T) {
	body := []byte(`{"choices":[{"index":0,"delta":{"refusal":"declined","audio":{"data":"YQ==","transcript":"voice"},"reasoning_content":"thought"},"logprobs":{"content":[{"token":"word","logprob":-0.1}]}}]}`)
	var response dto.ChatCompletionsStreamResponse
	require.NoError(t, kitutil.Unmarshal(body, &response))
	result, err := ConvertStreamResponse(context.Background(), nil, types.RelayFormatOpenAI, &response)
	require.NoError(t, err)
	encoded, err := kitutil.Marshal(result.Value)
	require.NoError(t, err)
	for _, path := range []string{"choices.0.delta.refusal", "choices.0.delta.audio", "choices.0.delta.reasoning_content", "choices.0.logprobs"} {
		assert.JSONEq(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(encoded, path).Raw)
	}
	state, err := NewResponseStreamState(types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, ResponseStreamOptions{})
	require.NoError(t, err)
	_, err = ConvertStreamResponseChunk(context.Background(), nil, state, &response)
	require.ErrorContains(t, err, "choices[0].delta")
}
