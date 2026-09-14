package router

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayOllamaInterruptedStreamHasOneTerminalError(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
	}{
		{"Chat", "/v1/chat/completions", `{"model":"llama3.1","messages":[{"role":"user","content":"interrupt"}],"stream":true,"max_tokens":32}`},
		{"Responses", "/v1/responses", `{"model":"llama3.1","input":"interrupt","stream":true,"max_output_tokens":32}`},
		{"Anthropic", "/v1/messages", `{"model":"llama3.1","messages":[{"role":"user","content":"interrupt"}],"stream":true,"max_tokens":32}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			base, key, _ := setupGatewaySDK(t)
			request, err := http.NewRequest(http.MethodPost, base+test.path, strings.NewReader(test.body))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			request.Header.Set("Content-Type", "application/json")
			response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			require.Equal(t, http.StatusOK, response.StatusCode)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			text := string(body)
			assert.Contains(t, text, "hello")
			assert.NotContains(t, text, "response.completed")
			if test.name == "Chat" {
				assert.Equal(t, 1, strings.Count(text, `"code":"stream_interrupted"`), text)
				assert.NotContains(t, text, "[DONE]")
			} else {
				terminalCount := strings.Count(text, "event: error\n") + strings.Count(text, "event: response.failed\n") + strings.Count(text, "event: response.incomplete\n")
				assert.Equal(t, 1, terminalCount, text)
				assert.NotContains(t, text, "event: message_stop")
			}
		})
	}
}
