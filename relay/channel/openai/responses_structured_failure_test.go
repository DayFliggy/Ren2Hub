package openai

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestResponsesStructuredFailuresAgreeAcrossTransports(t *testing.T) {
	for _, code := range []string{`"rate_limit_exceeded"`, `429`, `"503"`, `399`, `600`} {
		for _, transport := range []string{"json", "native_json", "native_stream", "stream", "buffered"} {
			t.Run(code+"/"+transport, func(t *testing.T) {
				body := fmt.Sprintf(`{"id":"resp_failed","status":"failed","error":{"code":%s,"type":"provider_error","message":"try later","param":"model"},"usage":{"input_tokens":12,"output_tokens":0,"total_tokens":12}}`, code)
				streaming := transport == "native_stream" || transport == "stream"
				if transport != "json" && transport != "native_json" {
					body = `data: {"type":"response.failed","response":` + body + "}\n\n"
				}
				c, recorder, response, info := newResponsesChatTestContext(t, body, streaming)
				handler := OaiResponsesToChatHandler
				switch transport {
				case "native_json":
					handler = OaiResponsesHandler
				case "native_stream":
					handler = OaiResponsesStreamHandler
				case "stream":
					handler = OaiResponsesToChatStreamHandler
				case "buffered":
					handler = OaiResponsesToChatBufferedStreamHandler
				}
				usage, apiErr := handler(c, info, response)
				require.NotNil(t, apiErr)
				require.NotNil(t, usage)
				want := 502
				if code == `429` {
					want = 429
				}
				if code == `"503"` {
					want = 503
				}
				require.Equal(t, want, apiErr.StatusCode)
				exposed := apiErr.ToOpenAIError()
				require.Equal(t, "provider_error", exposed.Type)
				require.Equal(t, "try later", exposed.Message)
				require.Equal(t, "model", exposed.Param)
				require.Equal(t, 12, usage.PromptTokens)
				require.False(t, info.HasValidOutput())
				require.Empty(t, recorder.Body.String())
			})
		}
	}
}
