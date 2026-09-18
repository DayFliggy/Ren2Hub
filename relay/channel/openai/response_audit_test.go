package openai

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOneCompletedChoiceCannotCompleteOtherPendingChoice(t *testing.T) {
	body := chatDeliverySSE(`{"choices":[{"index":0,"delta":{"content":"first"},"finish_reason":"stop"},{"index":1,"delta":{"content":"partial"}}]}`, "[DONE]")
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatGemini} {
		recorder := httptest.NewRecorder()
		_, _, _, apiErr := runChatDelivery(t, target, recorder, io.NopCloser(strings.NewReader(body)), nil)
		require.NotNil(t, apiErr)
		assert.NotContains(t, recorder.Body.String(), "[DONE]")
		assert.NotContains(t, recorder.Body.String(), `"finishReason":"STOP"`)
	}
}

func TestBufferedResponsesKeepsHostedItemsAndIndependentAnnotations(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"ws_1","type":"web_search_call","status":"completed","action":{"type":"search","query":"fixture"}}}`,
		`data: {"type":"response.output_text.delta","output_index":1,"content_index":0,"item_id":"msg_1","delta":"answer"}`,
		`data: {"type":"response.output_text.annotation.added","output_index":1,"content_index":0,"annotation_index":0,"annotation":{"type":"url_citation","url":"https://example.test","title":"Source","start_index":0,"end_index":6}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[],"usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}}`,
	}, "\n\n") + "\n\n"
	c, recorder, response, info := newResponsesChatTestContext(t, body, false)
	usage, apiErr := OaiResponsesToChatBufferedStreamHandler(c, info, response)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	assert.Equal(t, 8, usage.TotalTokens)
	assert.Contains(t, recorder.Body.String(), "https://example.test")
	assert.NotContains(t, recorder.Body.String(), "tool_calls")
	value, exists := c.Get("response_conversion_diagnostics")
	require.True(t, exists, "Chat cannot expose hosted execution but must declare its omission")
	assert.NotEmpty(t, value)

	c, recorder, response, info = newResponsesChatTestContext(t, body, false)
	info.ConversionPolicy = types.ConversionLossPolicyStrict
	_, apiErr = OaiResponsesToChatBufferedStreamHandler(c, info, response)
	require.NotNil(t, apiErr)
	assert.True(t, types.IsSkipRetryError(apiErr))
	assert.Empty(t, recorder.Body.String())
}

func TestFailedResponsesJSONRetainsObservedUsage(t *testing.T) {
	body := `{"id":"resp_1","status":"failed","error":{"type":"server_error","message":"fixture"},"output":[],"usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}`
	c, _, response, info := newResponsesChatTestContext(t, body, false)
	usage, apiErr := OaiResponsesToChatHandler(c, info, response)
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.StatusCode)
	require.NotNil(t, usage)
	assert.Equal(t, 8, usage.TotalTokens)
}

func TestChatJSONMissingTerminalOrEmbeddedFailureNeverReturnsSuccess(t *testing.T) {
	for _, body := range []string{
		`{"choices":[{"index":0,"message":{"content":"partial"}}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
		`{"error":{"type":"server_error","message":"fixture"},"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`,
	} {
		c, recorder, response, info := newResponsesChatTestContext(t, body, false)
		usage, apiErr := OaiChatToResponsesHandler(c, info, response)
		require.NotNil(t, apiErr)
		assert.Equal(t, 502, apiErr.StatusCode)
		require.NotNil(t, usage)
		assert.Equal(t, 8, usage.TotalTokens)
		assert.Empty(t, recorder.Body.String())
	}
}

func TestNativeChatEmbeddedFailureRetainsUsageWithErrorStatus(t *testing.T) {
	body := `{"error":{"type":"server_error","message":"fixture"},"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`
	c, recorder, response, info := newResponsesChatTestContext(t, body, false)
	usage, apiErr := OpenaiHandler(c, info, response)
	require.NotNil(t, apiErr)
	assert.Equal(t, 502, apiErr.StatusCode)
	require.NotNil(t, usage)
	assert.Equal(t, 8, usage.TotalTokens)
	assert.Empty(t, recorder.Body.String())
}
