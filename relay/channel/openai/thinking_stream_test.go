package openai

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThinkingStreamPreservesReasoningAndTextInSameFrame(t *testing.T) {
	for _, field := range []string{"reasoning_content", "reasoning"} {
		t.Run(field, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body := chatDeliverySSE(`{"id":"chat_think","choices":[{"index":0,"delta":{"`+field+`":"reason","content":"answer"},"finish_reason":"stop"}]}`, "[DONE]")
			_, info, _, apiErr := runChatDelivery(t, types.RelayFormatOpenAI, recorder, io.NopCloser(strings.NewReader(body)), enableThinkingStream)
			require.Nil(t, apiErr)
			assert.Equal(t, "<think>\nreason\n</think>\nanswer", chatDeliveryText(recorder.Body.String(), types.RelayFormatOpenAI))
			assert.Len(t, chatDeliveryFrames(recorder.Body.String()), 1)
			assert.Equal(t, 1, chatDeliveryTerminals(recorder.Body.String(), types.RelayFormatOpenAI))
			assert.Empty(t, info.OpenThinkingChoices)
			assert.True(t, info.HasValidOutput())
			assert.NotContains(t, recorder.Body.String(), `"`+field+`"`)
		})
	}
}

func TestThinkingStreamTracksEachChoiceAcrossReorderedFrames(t *testing.T) {
	recorder := httptest.NewRecorder()
	body := chatDeliverySSE(
		`{"id":"chat_choices","choices":[{"index":0,"delta":{"reasoning_content":"reason0"}},{"index":1,"delta":{"reasoning_content":"reason1"}}]}`,
		`{"choices":[{"index":1,"delta":{"reasoning_content":" continued","content":"answer1"}},{"index":0,"delta":{"content":"answer0"}}]}`,
		`{"choices":[{"index":1,"delta":{},"finish_reason":"length"},{"index":0,"delta":{},"finish_reason":"stop"}]}`, "[DONE]")
	_, info, _, apiErr := runChatDelivery(t, types.RelayFormatOpenAI, recorder, io.NopCloser(strings.NewReader(body)), enableThinkingStream)
	require.Nil(t, apiErr)
	text := map[int]string{}
	for _, frame := range chatDeliveryFrames(recorder.Body.String()) {
		for _, choice := range frame.Get("choices").Array() {
			text[int(choice.Get("index").Int())] += choice.Get("delta.content").String()
		}
	}
	assert.Equal(t, map[int]string{
		0: "<think>\nreason0\n</think>\nanswer0",
		1: "<think>\nreason1 continued\n</think>\nanswer1",
	}, text)
	assert.Len(t, chatDeliveryFrames(recorder.Body.String()), 3)
	assert.Empty(t, info.OpenThinkingChoices)
}

func TestThinkingStreamClosingTagDoesNotDuplicateToolDelta(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"tool_only", ""},
		{"text_and_tool", `"content":"answer",`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body := chatDeliverySSE(
				`{"id":"chat_think","choices":[{"index":0,"delta":{"reasoning_content":"reason"}}]}`,
				`{"choices":[{"index":0,"delta":{`+tc.content+`"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo","arguments":"{\"value\":0}"}}]},"finish_reason":"tool_calls"}]}`, "[DONE]")
			_, info, _, apiErr := runChatDelivery(t, types.RelayFormatOpenAI, recorder, io.NopCloser(strings.NewReader(body)), enableThinkingStream)
			require.Nil(t, apiErr)
			frames := chatDeliveryFrames(recorder.Body.String())
			require.Len(t, frames, 2)
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"call_a"`))
			assert.Equal(t, `{"value":0}`, frames[1].Get("choices.0.delta.tool_calls.0.function.arguments").String())
			assert.Equal(t, "tool_calls", frames[1].Get("choices.0.finish_reason").String())
			want := "<think>\nreason\n</think>\n"
			if tc.content != "" {
				want += "answer"
			}
			assert.Equal(t, want, chatDeliveryText(recorder.Body.String(), types.RelayFormatOpenAI))
			assert.Empty(t, info.OpenThinkingChoices)
		})
	}
}

func TestThinkingStreamFailedAttemptResetsChoiceState(t *testing.T) {
	writer := &chatDeliveryFailureWriter{ResponseRecorder: httptest.NewRecorder(), failAt: 1}
	body := chatDeliverySSE(`{"choices":[{"index":7,"delta":{"reasoning_content":"failed"}}]}`)
	_, info, _, apiErr := runChatDelivery(t, types.RelayFormatOpenAI, writer, io.NopCloser(strings.NewReader(body)), enableThinkingStream)
	require.NotNil(t, apiErr)
	assert.False(t, info.HasValidOutput())
	assert.True(t, info.OpenThinkingChoices[7])
	require.NoError(t, info.ResetAttemptState())
	assert.Nil(t, info.OpenThinkingChoices)
	assert.True(t, info.IsFirstThinkingContent)
	assert.False(t, info.HasSentThinkingContent)
	assert.False(t, info.SendLastThinkingContent)
}

func enableThinkingStream(info *relaycommon.RelayInfo) {
	info.ChannelSetting.ThinkingToContent = true
	info.ThinkingContentInfo.IsFirstThinkingContent = true
}
