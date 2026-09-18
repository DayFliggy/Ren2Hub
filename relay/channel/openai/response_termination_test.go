package openai

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResponsesLimitedResultsAgreeAcrossTransports(t *testing.T) {
	for _, reason := range []string{"max_output_tokens", "content_filter"} {
		for _, transport := range []string{"json", "buffered", "multiline", "stream"} {
			t.Run(reason+"/"+transport, func(t *testing.T) {
				body := `{"id":"resp_fixture","model":"gpt-test","status":"incomplete","incomplete_details":{"reason":"` + reason + `"},"output":[{"type":"message","id":"msg_fixture","role":"assistant","content":[{"type":"output_text","text":"partial"}]}],"usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}`
				if transport != "json" {
					body = "data: {\"type\":\"response.incomplete\",\"response\":" + body + "}\n\n"
				}
				if transport == "multiline" {
					body = strings.Replace(body, ",\"response\":", ",\ndata: \"response\":", 1)
				}
				c, recorder, response, info := newResponsesChatTestContext(t, body, transport == "stream")
				handler := OaiResponsesToChatHandler
				if transport == "stream" {
					handler = OaiResponsesToChatStreamHandler
				} else if transport != "json" {
					handler = OaiResponsesToChatBufferedStreamHandler
				}
				usage, apiErr := handler(c, info, response)
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, 8, usage.TotalTokens)
				assert.Contains(t, recorder.Body.String(), "partial")
				finish := "length"
				if reason == "content_filter" {
					finish = "content_filter"
				}
				assert.Contains(t, recorder.Body.String(), `"finish_reason":"`+finish+`"`)
				assert.NotContains(t, recorder.Body.String(), "response.failed")
			})
		}
	}
}

func TestUnknownIncompleteIsLocalConversionFailureWithUsage(t *testing.T) {
	for _, transport := range []string{"json", "buffered", "stream"} {
		t.Run(transport, func(t *testing.T) {
			body := `{"id":"resp_test","model":"gpt-test","status":"incomplete","incomplete_details":{"reason":"future_reason"},"output":[],"usage":{"input_tokens":3,"output_tokens":5,"total_tokens":8}}`
			if transport != "json" {
				body = "data: {\"type\":\"response.incomplete\",\"response\":" + body + "}\n\n"
			}
			c, recorder, response, info := newResponsesChatTestContext(t, body, transport == "stream")
			handler := OaiResponsesToChatHandler
			if transport == "buffered" {
				handler = OaiResponsesToChatBufferedStreamHandler
			}
			if transport == "stream" {
				handler = OaiResponsesToChatStreamHandler
			}
			usage, apiErr := handler(c, info, response)
			require.NotNil(t, apiErr)
			assert.Equal(t, 502, apiErr.StatusCode)
			assert.Equal(t, helper.ResponseConversionErrorCode, apiErr.GetErrorCode())
			assert.True(t, types.IsSkipRetryError(apiErr))
			require.NotNil(t, usage)
			assert.Equal(t, 8, usage.TotalTokens)
			assert.Empty(t, recorder.Body.String())
		})
	}
}
