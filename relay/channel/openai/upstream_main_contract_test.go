package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNativeChatForceFormatPreservesStructuredOutputFields(t *testing.T) {
	body := `{"id":"chat","model":"model","choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"thought","refusal":"declined","audio":{"id":"audio_1","data":"YQ==","transcript":"voice","expires_at":123}},"logprobs":{"content":[{"token":"answer","logprob":-0.1}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`
	c, recorder, response, info := newResponsesChatTestContext(t, body, false)
	info.ChannelSetting.ForceFormat = true
	_, apiErr := OpenaiHandler(c, info, response)
	require.Nil(t, apiErr)
	for _, path := range []string{"choices.0.message.refusal", "choices.0.message.audio", "choices.0.message.reasoning_content", "choices.0.logprobs"} {
		assert.JSONEq(t, gjson.Get(body, path).Raw, gjson.Get(recorder.Body.String(), path).Raw, path)
	}
}

func TestRetryCannotReuseResponsesCustomToolState(t *testing.T) {
	_, _, _, info := newResponsesChatTestContext(t, "", false)
	info.SetResponsesToolState(&convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}})
	require.True(t, info.ResponsesToolState().IsCustomTool("exec"))
	require.NoError(t, info.ResetAttemptState())
	assert.Nil(t, info.ResponsesToolState())
}
